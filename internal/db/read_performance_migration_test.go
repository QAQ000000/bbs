package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReadPerformanceMigrationAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Read performance (schema 16).")
	if len(base) != 2 {
		t.Fatal("missing schema16 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 16 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('performance-migration','hash')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
		var valid bool
		if err = pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations)=16
		 AND (SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
		 WHERE c.relname=ANY($1) AND i.indisvalid)=4
		 AND EXISTS(SELECT 1 FROM users WHERE username='performance-migration')`, []string{"threads_public_latest_idx", "threads_public_new_idx", "threads_public_author_idx", "posts_public_created_idx"}).Scan(&valid); err != nil || !valid {
			t.Fatal(valid, err)
		}
	}
}
