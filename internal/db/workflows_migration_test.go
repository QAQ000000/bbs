// SPDX-License-Identifier: AGPL-3.0-or-later
package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkflowsMigrationPreservesLegacyDraftsAndNotifications(t *testing.T) {
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
	if _, err = pool.Exec(ctx, `DROP SCHEMA public CASCADE;CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	marker := "-- Forum workflow metadata and durable, deduplicated in-app notifications."
	parts := strings.Split(string(assets.SchemaFile()), marker)
	if len(parts) != 2 {
		t.Fatal("workflow schema marker missing")
	}
	if _, err = pool.Exec(ctx, parts[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 8 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	var uid int64
	if err = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES('workflow-legacy','test') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO drafts(user_id,context,content) VALUES($1,'new:1','legacy draft')`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO notifications(uid,from_uid,from_name,type,thread_id,post_id,excerpt,read) VALUES($1,0,'legacy','reply',1,1,'legacy notification',true)`, uid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var content, subject, excerpt, scope string
	var read bool
	var version int
	if err = pool.QueryRow(ctx, `SELECT d.content,d.subject,n.excerpt,n.scope,n.read,(SELECT max(version) FROM schema_migrations) FROM drafts d JOIN notifications n ON n.uid=d.user_id WHERE d.user_id=$1`, uid).Scan(&content, &subject, &excerpt, &scope, &read, &version); err != nil || content != "legacy draft" || subject != "" || excerpt != "legacy notification" || scope != "content" || !read || version != 16 {
		t.Fatal(content, subject, excerpt, scope, read, version, err)
	}
	var triggers int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgname LIKE 'notify_%'`).Scan(&triggers); err != nil || triggers != 5 {
		t.Fatal("notification triggers", triggers, err)
	}
}
