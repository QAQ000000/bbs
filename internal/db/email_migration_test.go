package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmailMigrationFromNineAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Durable email outbox. Tokens are encrypted with an application-owned key.")
	if len(base) != 2 {
		t.Fatal("missing schema10 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 10 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('mail-migration','test');INSERT INTO email_jobs(uid,kind,recipient,dedup_key,sealed_token,expires_at) VALUES(1,'email_verify','test@example.test','migration-test','encrypted',now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var version, count int
	var sealed string
	if err = pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),(SELECT count(*) FROM email_jobs),sealed_token FROM email_jobs WHERE dedup_key='migration-test'`).Scan(&version, &count, &sealed); err != nil || version != 16 || count != 1 || sealed != "encrypted" {
		t.Fatal(version, count, sealed, err)
	}
}
