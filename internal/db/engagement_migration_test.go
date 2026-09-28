package db

import (
	"strings"
	"testing"

	"dzforum/assets"
)

func latestSchemaVersion() int {
	migrations := assets.Migrations()
	return migrations[len(migrations)-1].Version
}

func TestEngagementMigrationPreservesRulesAndRestart(t *testing.T) {
	pool := migrationPool(t)
	ctx := t.Context()
	base, _, ok := strings.Cut(string(assets.SchemaFile()), "-- Polls and versioned operating rules for the engagement features.")
	if !ok {
		t.Fatal("missing schema18 boundary")
	}
	if _, err := pool.Exec(ctx, base); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 18 {
			continue
		}
		if _, err := pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE membership_config SET version=7,body=jsonb_set(jsonb_set(body,'{levels,0,permissions,thread.create}','false'),'{levels,0,permissions,poll.vote}','false') WHERE id`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var preserved, ready bool
	var version int
	if err := pool.QueryRow(ctx, `SELECT version,(body#>>'{levels,0,permissions,thread.create}')::boolean=false AND (body#>>'{levels,0,permissions,poll.create}')::boolean=false AND (body#>>'{levels,0,permissions,bounty.create}')::boolean=false AND (body#>>'{levels,0,permissions,poll.vote}')::boolean=false AND (body#>>'{levels,0,permissions,checkin.claim}')::boolean=true, to_regclass('poll_ballots') IS NOT NULL AND to_regclass('thread_bounties') IS NOT NULL AND to_regclass('checkin_records') IS NOT NULL FROM membership_config WHERE id`).Scan(&version, &preserved, &ready); err != nil || version != 8 || !preserved || !ready {
		t.Fatal(version, preserved, ready, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE engagement_config SET version=3,body=jsonb_set(body,'{poll,enabled}','false') WHERE id`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),version=3 AND (body#>>'{poll,enabled}')::boolean=false AND (SELECT version=8 FROM membership_config WHERE id) FROM engagement_config WHERE id`).Scan(&version, &preserved); err != nil || version != latestSchemaVersion() || !preserved {
		t.Fatal(version, preserved, err)
	}
}
