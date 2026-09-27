package store

import (
	"context"
	"time"
)

type DerivedRepairResult struct {
	SearchJobs int64
	ForumJobs  int64
}

// QueueDerivedRepair schedules an explicit full reconciliation. Existing search
// jobs keep their original age and retry state. Consumers recheck visibility and
// content under their normal locks, so repair never writes a stale search vector.
func (s *Store) QueueDerivedRepair(ctx context.Context) (DerivedRepairResult, error) {
	var result DerivedRepairResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// Serialize maintenance producers; ordinary publishers remain independent.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x474f425250)); err != nil {
		return result, err
	}
	search, err := tx.Exec(ctx, `INSERT INTO search_index_events(post_id) SELECT id FROM posts ORDER BY id ON CONFLICT(post_id) DO NOTHING`)
	if err != nil {
		return result, err
	}
	forums, err := tx.Exec(ctx, `INSERT INTO forum_stat_events(forum_id) SELECT f.id FROM forums f WHERE NOT EXISTS(SELECT 1 FROM forum_stat_events e WHERE e.forum_id=f.id) ORDER BY f.id`)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	result.SearchJobs, result.ForumJobs = search.RowsAffected(), forums.RowsAffected()
	return result, nil
}
