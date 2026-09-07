package db

import (
	"context"
	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestSessionsMigrationPreservesLegacyAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Device sessions: public identifiers are independent of authentication tokens.")
	if len(base) != 2 {
		t.Fatal("missing schema11 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 11 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('legacy-device','test');INSERT INTO sessions(token,user_id,csrf,expires_at) VALUES('hashed-token',1,'csrf',now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var id int64
	var preserved bool
	if err = pool.QueryRow(ctx, `SELECT id,last_seen_at=created_at AND revoked_at IS NULL FROM sessions WHERE token='hashed-token'`).Scan(&id, &preserved); err != nil || id < 1 || !preserved {
		t.Fatal(id, preserved, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE sessions SET device_name='custom',revoked_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),device_name='custom' AND revoked_at IS NOT NULL FROM sessions WHERE id=$1`, id).Scan(&version, &preserved); err != nil || version != 15 || !preserved {
		t.Fatal(version, preserved, err)
	}
}
