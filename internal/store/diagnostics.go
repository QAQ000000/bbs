package store

import "dzforum/internal/db"

import "context"

func (s *Store) DatabasePoolStats() db.PoolStats { return db.Snapshot(s.pool) }

func (s *Store) DatabaseLockWaits(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename=current_user AND wait_event_type='Lock'`).Scan(&n)
	return n, err
}
