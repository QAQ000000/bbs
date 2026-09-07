package db

import (
	"context"
	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestPointsMigrationBaselineSnapshotsAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Independent points accounts and append-only accounting entries.")
	if len(base) != 2 {
		t.Fatal("missing schema13 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 13 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('points-migration','test');INSERT INTO member_experience(user_id,source,kind,delta,rule_version,reason) VALUES(1,'legacy-post','thread',5,1,'legacy');SELECT member_enqueue(1,'thread','queued-before-points',true)`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var baseline int
	var legacy bool
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM points_ledger WHERE source='legacy-post' AND delta=0),points_rule IS NULL FROM member_events WHERE source='queued-before-points'`).Scan(&baseline, &legacy); err != nil || baseline != 1 || !legacy {
		t.Fatal(baseline, legacy, err)
	}
	if _, err = pool.Exec(ctx, `SELECT member_enqueue(1,'thread','new-event',true);UPDATE points_config SET version=version+1,body=jsonb_set(body,'{rules,thread,points}','3') WHERE id;INSERT INTO points_accounts(user_id,balance) VALUES(1,9);INSERT INTO points_ledger(user_id,source,kind,delta,balance_after,frozen_after,rule_version,reason,event_at) VALUES(1,'preserved','admin',9,9,0,0,'test',now())`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var version, amount, snapshot, balance int
	if err = pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),(body->'rules'->'thread'->>'points')::int,(SELECT (points_rule->>'points')::int FROM member_events WHERE source='new-event'),(SELECT balance FROM points_accounts WHERE user_id=1) FROM points_config WHERE id`).Scan(&version, &amount, &snapshot, &balance); err != nil || version != 15 || amount != 3 || snapshot != 1 || balance != 9 {
		t.Fatal(version, amount, snapshot, balance, err)
	}
}
