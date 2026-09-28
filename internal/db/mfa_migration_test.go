package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMFAMigrationPreservesProtectionAndRestart(t *testing.T) {
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
	base := strings.Split(string(assets.SchemaFile()), "-- Transactional MFA enrollment and browser-bound login challenges.")
	if len(base) != 2 {
		t.Fatal("missing schema14 boundary")
	}
	if _, err = pool.Exec(ctx, base[0]); err != nil {
		t.Fatal(err)
	}
	for _, m := range assets.Migrations() {
		if m.Version >= 14 {
			continue
		}
		if _, err = pool.Exec(ctx, m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES('mfa-migration','password-hash'); INSERT INTO user_mfa(user_id,secret_cipher,enabled,last_step,recovery_hashes) VALUES(1,'encrypted-secret',true,42,ARRAY[repeat('a',40)]); INSERT INTO mfa_challenges(user_id,password_hash,expires_at) VALUES(1,'password-hash',now()+interval '5 minutes')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var protected, invalidated bool
	if err = pool.QueryRow(ctx, `SELECT enabled AND secret_cipher='encrypted-secret' AND last_step=42 AND cardinality(recovery_hashes)=0,(SELECT bool_and(used_at IS NOT NULL) FROM mfa_challenges) FROM user_mfa WHERE user_id=1`).Scan(&protected, &invalidated); err != nil || !protected || !invalidated {
		t.Fatal(protected, invalidated, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE user_mfa SET version='v2',recovery_hashes=ARRAY[repeat('b',64)],last_step=43 WHERE user_id=1; INSERT INTO mfa_challenges(user_id,password_hash,expires_at,token_hash,csrf_hash,mfa_version,attempts) VALUES(1,'password-hash',now()+interval '5 minutes','challenge-hash','csrf-hash','v2',3); INSERT INTO mfa_attempts(user_id,scope,attempts) VALUES(1,'verify',7)`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),enabled AND secret_cipher='encrypted-secret' AND last_step=43 AND version='v2' AND cardinality(recovery_hashes)=1 AND (SELECT attempts=7 FROM mfa_attempts WHERE user_id=1 AND scope='verify') AND (SELECT used_at IS NULL AND attempts=3 FROM mfa_challenges WHERE token_hash='challenge-hash') FROM user_mfa WHERE user_id=1`).Scan(&version, &protected); err != nil || version != latestSchemaVersion() || !protected {
		t.Fatal(version, protected, err)
	}
}
