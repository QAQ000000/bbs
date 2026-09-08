package store

import "dzforum/internal/db"

import "context"

func (s *Store) DatabasePoolStats() db.PoolStats { return db.Snapshot(s.pool) }

func (s *Store) DatabaseLockWaits(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename=current_user AND wait_event_type='Lock'`).Scan(&n)
	return n, err
}

type DatabaseWorkloadStats struct {
	Transactions  int64   `json:"transactions"`
	ReadIO        int64   `json:"readIO"`
	HitIO         int64   `json:"hitIO"`
	CacheHitRatio float64 `json:"cacheHitRatio"`
}

func (s *Store) DatabaseWorkloadStats(ctx context.Context) (DatabaseWorkloadStats, error) {
	var v DatabaseWorkloadStats
	err := s.pool.QueryRow(ctx, `SELECT xact_commit+xact_rollback, blks_read, blks_hit, CASE WHEN blks_read+blks_hit=0 THEN 1 ELSE blks_hit::float8/(blks_read+blks_hit) END FROM pg_stat_database WHERE datname=current_database()`).Scan(&v.Transactions, &v.ReadIO, &v.HitIO, &v.CacheHitRatio)
	return v, err
}
