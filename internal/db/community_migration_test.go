// SPDX-License-Identifier: AGPL-3.0-or-later
package db

import (
	"context"
	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestCommunityMigrationFromEightAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Community relations: follows, tags, subscriptions and direct messages.")
	if len(base) != 2 {
		t.Fatal("missing schema9 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 9 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('community-a','test'),('community-b','test')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO user_follows(follower_id,following_id) VALUES(1,2);
 INSERT INTO conversations(id) VALUES(1);
 INSERT INTO conversation_members(conversation_id,uid) VALUES(1,1),(1,2);
 INSERT INTO conversation_pair_states(conversation_id,initiator_id,recipient_id) VALUES(1,1,2);
 INSERT INTO messages(conversation_id,sender_id,body) VALUES(1,1,'preserved');`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var text string
	var follows, version int
	if err = pool.QueryRow(ctx, `SELECT body,(SELECT count(*) FROM user_follows),(SELECT max(version) FROM schema_migrations) FROM messages WHERE conversation_id=1`).Scan(&text, &follows, &version); err != nil || text != "preserved" || follows != 1 || version != 9 {
		t.Fatal(text, follows, version, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO conversations(id) VALUES(2)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO conversation_pair_states(conversation_id,initiator_id,recipient_id) VALUES(2,2,1)`); err == nil {
		t.Fatal("duplicate reversed pair allowed")
	}
}
