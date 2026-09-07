package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkflowIntegrityMigrationAndRestart(t *testing.T) {
	dsn := os.Getenv("FORUM_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("requires dedicated migration database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "gobbs_test_") {
		t.Fatal("unsafe migration database")
	}
	ctx := context.Background()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	base := strings.Split(string(assets.SchemaFile()), "-- Verified recovery address changes (schema 15).")
	if len(base) != 2 {
		t.Fatal("missing schema15 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 15 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('flow-migration','hash');
 INSERT INTO categories(name) VALUES('category'); INSERT INTO forums(category_id,name) VALUES(1,'forum');
 INSERT INTO threads(forum_id,author_id,title) VALUES(1,1,'thread');
 INSERT INTO posts(thread_id,author_id,floor,content_md,content_html) VALUES(1,1,1,'body','');
 UPDATE subscription_events SET created_at='2026-01-01',cursor_uid=7,completed=true;
 INSERT INTO subscription_deliveries(uid,post_id) VALUES(1,1);
 INSERT INTO sessions(token,user_id,csrf,expires_at) VALUES('hash',1,'csrf',now()+interval '1 day');`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err = pool.QueryRow(ctx, `SELECT NOT completed AND cursor_uid=7 AND created_at='2026-01-01' AND (SELECT count(*) FROM subscription_deliveries)=1 FROM subscription_events WHERE post_id=1`).Scan(&valid); err != nil || !valid {
		t.Fatal("lost delivery state", valid, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE subscription_events SET completed=true;
 INSERT INTO email_changes(uid,new_email,old_email,password_hash,session_id,token_hash,expires_at) SELECT 1,'new@example.test','','hash',id,'confirmation',now()+interval '30 minutes' FROM sessions WHERE token='hash';
 INSERT INTO email_jobs(uid,kind,recipient,token_hash,sealed_token,dedup_key,expires_at) VALUES(1,'email_change','new@example.test','confirmation','ciphertext','change',now()+interval '30 minutes'),(1,'email_changed','old@example.test','','','notice',now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT completed AND cursor_uid=7 AND (SELECT max(version) FROM schema_migrations)=15 AND (SELECT token_hash='confirmation' FROM email_changes WHERE uid=1) AND (SELECT count(*) FROM email_jobs)=2 FROM subscription_events WHERE post_id=1`).Scan(&valid); err != nil || !valid {
		t.Fatal("restart changed completed/pending state", valid, err)
	}
}
