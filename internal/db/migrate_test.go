package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"dzforum/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("FORUM_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("requires dedicated migration database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "gobbs_test_") {
		t.Fatal("unsafe migration database")
	}
	pool, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(t.Context(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestMigrationConcurrentStartupAndNoDDLOnRestart(t *testing.T) {
	pool := migrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 6)
	for range 6 {
		go func() { <-start; results <- Migrate(ctx, pool) }()
	}
	close(start)
	for range 6 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != 1+len(assets.Migrations()) {
		t.Fatalf("migration ledger count=%d err=%v", count, err)
	}
	// A normal restart must not touch/lock existing business tables.
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err := holder.Exec(ctx, `LOCK TABLE users IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	probe, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := Migrate(probe, pool); err != nil {
		t.Fatalf("restart attempted business DDL: %v", err)
	}
}

func TestMigrationVersionWriteFailureRollsBackDDL(t *testing.T) {
	pool := migrationPool(t)
	ctx := t.Context()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	migrations := assets.Migrations()
	next := migrations[len(migrations)-1].Version + 1
	migrations = append(migrations, assets.Migration{Version: next, Name: "atomicity-probe", SQL: `CREATE TABLE migration_atomicity_probe(id int)`})
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_migration_record() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected ledger failure'; END$$; CREATE TRIGGER reject_migration_record BEFORE INSERT ON schema_migrations FOR EACH ROW EXECUTE FUNCTION reject_migration_record()`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, pool, string(assets.SchemaFile()), migrations); err == nil || !strings.Contains(err.Error(), "record migration") {
		t.Fatalf("expected version-record failure, got %v", err)
	}
	var clean bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('migration_atomicity_probe') IS NULL AND (SELECT max(version) FROM schema_migrations)=$1`, next-1).Scan(&clean); err != nil || !clean {
		t.Fatalf("failed migration partially committed: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_migration_record ON schema_migrations; DROP FUNCTION reject_migration_record()`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, pool, string(assets.SchemaFile()), migrations); err != nil {
		t.Fatalf("retry after recovery failed: %v", err)
	}
}

func TestMigrationFailedBootstrapLeavesNoPartialSchema(t *testing.T) {
	pool := migrationPool(t)
	migrations := assets.Migrations()
	next := migrations[len(migrations)-1].Version + 1
	migrations = append(migrations, assets.Migration{Version: next, Name: "failure-probe", SQL: `CREATE TABLE migration_failure_probe(id int); SELECT 1/0`})
	if err := migrate(t.Context(), pool, string(assets.SchemaFile()), migrations); err == nil {
		t.Fatal("injected failure was ignored")
	}
	var clean bool
	if err := pool.QueryRow(t.Context(), `SELECT to_regclass('users') IS NULL AND to_regclass('schema_migrations') IS NULL AND to_regclass('migration_failure_probe') IS NULL`).Scan(&clean); err != nil || !clean {
		t.Fatalf("failed bootstrap left partial schema: %v", err)
	}
	if err := Migrate(t.Context(), pool); err != nil {
		t.Fatalf("bootstrap retry failed: %v", err)
	}
}

func TestMigrationLockWaitCanBeCancelled(t *testing.T) {
	pool := migrationPool(t)
	holder, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err := holder.Exec(t.Context(), `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if err := Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), "acquire migration lock") {
		t.Fatalf("expected cancelled lock acquisition: %v", err)
	}
	var clean bool
	if err := pool.QueryRow(t.Context(), `SELECT to_regclass('schema_migrations') IS NULL`).Scan(&clean); err != nil || !clean {
		t.Fatalf("blocked migrator changed schema: %v", err)
	}
	if err := holder.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), pool); err != nil {
		t.Fatalf("cancelled migrator retained lock: %v", err)
	}
}

func TestMigrationRejectsUnsupportedHistory(t *testing.T) {
	for name, mutation := range map[string]string{
		"gap":    `DELETE FROM schema_migrations WHERE version=3`,
		"future": `INSERT INTO schema_migrations(version) SELECT max(version)+1 FROM schema_migrations`,
		"empty":  `DELETE FROM schema_migrations`,
	} {
		t.Run(name, func(t *testing.T) {
			pool := migrationPool(t)
			if err := Migrate(t.Context(), pool); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(t.Context(), pool); err == nil {
				t.Fatal("unsupported migration history was accepted")
			}
		})
	}
}

func TestDatabaseRejectsMissingDSN(t *testing.T) {
	for _, dsn := range []string{"", " \t\n"} {
		if pool, err := Open(t.Context(), dsn); err == nil || pool != nil || !strings.Contains(err.Error(), "FORUM_DSN is required") {
			t.Fatalf("missing DSN not rejected: %v", err)
		}
	}
}
