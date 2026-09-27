package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dzforum/assets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All GoBBS migrators in one database share this transaction-scoped lock.
const migrationLockID int64 = 0x474f424253

// Migrate serializes startup/maintenance migrators and commits DDL and version
// records atomically. Versioned databases use numbered migrations only; the
// complete schema is a bootstrap for new or unversioned databases, not a repair.
// Migrations must be transactional (no CREATE INDEX CONCURRENTLY, VACUUM, etc.).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate(ctx, pool, string(assets.SchemaFile()), assets.Migrations())
}

func migrate(ctx context.Context, pool *pgxpool.Pool, schema string, migrations []assets.Migration) error {
	latest := 1
	for _, m := range migrations {
		if m.Version != latest+1 {
			return fmt.Errorf("non-contiguous bundled migration: expected %d, got %d", latest+1, m.Version)
		}
		latest = m.Version
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect migration ledger: %w", err)
	}
	if !exists {
		if _, err := tx.Exec(ctx, schema); err != nil {
			return fmt.Errorf("bootstrap schema: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return fmt.Errorf("read migration versions: %w", err)
	}
	if len(versions) == 0 {
		return fmt.Errorf("migration ledger is empty; restore or repair it before upgrading")
	}
	for i, v := range versions {
		if v != i+1 || v > latest {
			return fmt.Errorf("unsupported migration ledger: expected version %d, found %d (binary supports %d)", i+1, v, latest)
		}
	}
	current := versions[len(versions)-1]
	var applied []assets.Migration
	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("apply migration %03d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, m.Version); err != nil {
			return fmt.Errorf("record migration %03d: %w", m.Version, err)
		}
		applied = append(applied, m)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	for _, m := range applied {
		slog.Info("已应用编号迁移", "version", m.Version, "name", m.Name)
	}
	return nil
}
