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

func TestMembershipMigrationReplacesLegacyTrust(t *testing.T) {
	dsn := os.Getenv("FORUM_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("requires a dedicated gobbs_test_ migration database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "gobbs_test_") {
		t.Fatal("unsafe migration test database")
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
	base := strings.Split(string(assets.SchemaFile()), "-- Membership configuration, transactional event queue and audit ledgers.")[0]
	if _, err = pool.Exec(ctx, base); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 6 {
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
	// Model a pre-membership database; the new schema no longer declares this column.
	if _, err = pool.Exec(ctx, `ALTER TABLE users ADD COLUMN trust_level smallint NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash,trust_level,days_visited,posts_read,post_count,group_id) VALUES('existing-member','test-hash',2,20,150,12,2) RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `WITH c AS (INSERT INTO categories(name) VALUES('legacy') RETURNING id), f AS (INSERT INTO forums(category_id,name) SELECT id,'legacy' FROM c RETURNING id), t AS (INSERT INTO threads(forum_id,author_id,title) SELECT id,$1,'legacy thread' FROM f RETURNING id), p AS (INSERT INTO posts(thread_id,author_id,floor,content_md,content_html) SELECT id,$1,1,'legacy','legacy' FROM t RETURNING thread_id) INSERT INTO thread_reads(user_id,thread_id,last_floor) SELECT $1,thread_id,1 FROM p`, uid); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var oldReads int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM member_read_posts WHERE user_id=$1`, uid).Scan(&oldReads); err != nil || oldReads != 0 {
		t.Fatal("legacy reading was imported into the new member ledger", err, oldReads)
	}
	var lid, role, days int
	var xp, reads int64
	if err = pool.QueryRow(ctx, `SELECT ms.level_id,ms.experience,u.group_id,u.days_visited,u.posts_read FROM member_states ms JOIN users u ON u.id=ms.user_id WHERE u.id=$1`, uid).Scan(&lid, &xp, &role, &days, &reads); err != nil {
		t.Fatal(err)
	}
	if lid != 0 || xp != 0 || role != 2 || days != 20 || reads != 150 {
		t.Fatalf("migration changed existing state: %d/%d/%d/%d/%d", lid, xp, role, days, reads)
	}
	var legacyColumn bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='users' AND column_name='trust_level')`).Scan(&legacyColumn); err != nil || legacyColumn {
		t.Fatal("legacy trust column remains", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE member_states SET level_id=4,locked=true,experience=5000 WHERE user_id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE membership_config SET version=2,body=jsonb_set(body,'{levels,0,name}','"自定义新人"') WHERE id`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var name string
	var locked bool
	var version int
	if err = pool.QueryRow(ctx, `SELECT ms.level_id,ms.locked,ms.experience,c.body->'levels'->0->>'name',c.version FROM member_states ms CROSS JOIN membership_config c WHERE ms.user_id=$1`, uid).Scan(&lid, &locked, &xp, &name, &version); err != nil {
		t.Fatal(err)
	}
	if lid != 4 || !locked || xp != 5000 || name != "自定义新人" || version != 2 {
		t.Fatal("restart reset membership configuration or state")
	}
	if err = pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 17 {
		t.Fatal("schema version was not advanced", err, version)
	}
}
