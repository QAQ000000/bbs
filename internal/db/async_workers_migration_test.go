package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAsyncWorkersMigration(t *testing.T) {
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
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	base, _, ok := strings.Cut(string(assets.SchemaFile()), "-- Asynchronous forum statistics (schema 17).")
	if !ok {
		t.Fatal("missing schema17 boundary")
	}
	exec(base)
	for _, m := range assets.Migrations() {
		if m.Version >= 17 {
			continue
		}
		exec(m.SQL)
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	// Apply the numbered migration directly before schema replay can mask missing DDL.
	exec(assets.MigrationSQL(17))
	exec(`INSERT INTO users(id,username,password_hash) OVERRIDING SYSTEM VALUE VALUES(1,'migration17','hash');
INSERT INTO categories(id,name) OVERRIDING SYSTEM VALUE VALUES(1,'category');
INSERT INTO forums(id,category_id,name) OVERRIDING SYSTEM VALUE VALUES(1,1,'forum');
INSERT INTO threads(id,forum_id,author_id,title) OVERRIDING SYSTEM VALUE VALUES(1,1,1,'preserved');
INSERT INTO posts(id,thread_id,author_id,floor,content_md,content_html) OVERRIDING SYSTEM VALUE VALUES(1,1,1,1,'preserved','');
INSERT INTO forum_stat_events(forum_id,attempts,last_sqlstate) VALUES(1,3,'P0001');
INSERT INTO search_index_events(post_id,attempts,last_sqlstate) VALUES(1,4,'P0001');
INSERT INTO analytics_snapshots(name,period_start,payload) VALUES('points',now(),'[{"score":42}]')`)
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
		var valid bool
		if err := pool.QueryRow(ctx, `SELECT
(SELECT max(version) FROM schema_migrations)=$1 AND
(SELECT count(*) FROM forum_stat_events WHERE attempts=3 AND last_sqlstate='P0001')=1 AND
(SELECT count(*) FROM search_index_events WHERE attempts=4 AND last_sqlstate='P0001')=1 AND
(SELECT payload->0->>'score' FROM analytics_snapshots WHERE name='points')='42'`, latestSchemaVersion()).Scan(&valid); err != nil || !valid {
			t.Fatalf("migration lost durable data: %v", err)
		}
	}
}
