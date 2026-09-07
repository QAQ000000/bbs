package store

import "dzforum/internal/db"

func (s *Store) DatabasePoolStats() db.PoolStats { return db.Snapshot(s.pool) }
