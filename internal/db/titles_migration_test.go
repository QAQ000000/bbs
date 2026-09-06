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

func TestTitlesMigrationPreservesMembershipAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Task-based honorary titles, independent of membership permissions.")[0]
	if _, err = pool.Exec(ctx, base); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 7 {
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
	if err = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES('existing-title-user','test-hash') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE member_states SET experience=12000,level_id=4,locked=true WHERE user_id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var tid int64
	if err = pool.QueryRow(ctx, `INSERT INTO titles(version,body) VALUES(3,'{"name":"custom","status":"active","mode":"manual"}') RETURNING id`).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO user_titles(user_id,title_id,status,source,rule_version) VALUES($1,$2,'earned','manual',3)`, uid, tid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO title_equipment(user_id,title_id) VALUES($1,$2)`, uid, tid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO title_jobs(title_id,rule_version,max_user_id) VALUES($1,3,$2)`, tid, uid); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var xp, level, version, jobs int
	var locked, equipped bool
	var name string
	err = pool.QueryRow(ctx, `SELECT ms.experience,ms.level_id,ms.locked,t.version,t.body->>'name',EXISTS(SELECT 1 FROM title_equipment WHERE user_id=$1 AND title_id=$2),(SELECT count(*) FROM title_jobs WHERE title_id=$2) FROM member_states ms CROSS JOIN titles t WHERE ms.user_id=$1 AND t.id=$2`, uid, tid).Scan(&xp, &level, &locked, &version, &name, &equipped, &jobs)
	if err != nil || xp != 12000 || level != 4 || !locked || version != 3 || name != "custom" || !equipped || jobs != 1 {
		t.Fatal("restart reset state", err, xp, level, locked, version, name, equipped, jobs)
	}
	var triggers int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgname LIKE 'title_%'`).Scan(&triggers); err != nil || triggers != 6 {
		t.Fatal("duplicate or missing triggers", triggers, err)
	}
}
