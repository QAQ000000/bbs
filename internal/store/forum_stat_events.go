package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ForumStatsQueueStatus struct {
	AsyncPublication bool    `json:"asyncPublication"`
	Pending          int64   `json:"pending"`
	Retrying         int64   `json:"retrying"`
	OldestAgeSeconds float64 `json:"oldestAgeSeconds"`
}

func (s *Store) ForumStatsQueueStatus(ctx context.Context) (ForumStatsQueueStatus, error) {
	v := ForumStatsQueueStatus{AsyncPublication: s.asyncForumStats}
	err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE attempts>0),
	 coalesce(extract(epoch FROM now()-min(created_at)),0)::float8 FROM forum_stat_events`).Scan(&v.Pending, &v.Retrying, &v.OldestAgeSeconds)
	return v, err
}

// QueueForumStats also acts as a retry request for existing failed events.
func (s *Store) QueueForumStats(ctx context.Context, fid, actor int64, ip string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO forum_stat_events(forum_id) SELECT id FROM forums WHERE id=$1 RETURNING id`, fid).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail,ip)
	 SELECT id,username,'forum.stats.reconcile',$2,$3 FROM users WHERE id=$1`, actor, fmt.Sprintf("forum=%d", fid), ip)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// QueueAllForumStats is periodic reconciliation, not a per-publication upsert.
func (s *Store) QueueAllForumStats(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO forum_stat_events(forum_id)
	 SELECT f.id FROM forums f WHERE NOT EXISTS(SELECT 1 FROM forum_stat_events e WHERE e.forum_id=f.id)`)
	return err
}

// One transaction owns a forum and a bounded set of invalidations. The database
// locks are the lease: process failure rolls back both statistics and deletion.
func (s *Store) ProcessForumStats(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var fid int64
	err = tx.QueryRow(ctx, `SELECT f.id FROM forums f JOIN LATERAL (
	 SELECT e.id,e.next_attempt_at FROM forum_stat_events e WHERE e.forum_id=f.id AND e.next_attempt_at<=now()
	 ORDER BY e.next_attempt_at,e.id LIMIT 1) e ON true
	 ORDER BY e.next_attempt_at,e.id LIMIT 1 FOR NO KEY UPDATE OF f SKIP LOCKED`).Scan(&fid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM forum_stat_events WHERE forum_id=$1 ORDER BY id LIMIT 1000 FOR UPDATE`, fid)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	// A savepoint lets ordinary SQL failures persist retry metadata atomically.
	work, err := tx.Begin(ctx)
	if err != nil {
		return 0, err
	}
	err = recomputeForumStats(ctx, work, fid)
	if err == nil {
		_, err = work.Exec(ctx, `DELETE FROM forum_stat_events WHERE id=ANY($1)`, ids)
	}
	if err != nil {
		cause := err
		if err = work.Rollback(ctx); err != nil {
			return fid, errors.Join(cause, err)
		}
		state := ""
		var pgErr *pgconn.PgError
		if errors.As(cause, &pgErr) {
			state = pgErr.Code
		}
		_, err = tx.Exec(ctx, `UPDATE forum_stat_events SET attempts=least(attempts+1,16),
		 next_attempt_at=now()+least(300,power(2,least(attempts+1,8))) * interval '1 second',last_sqlstate=$2
		 WHERE id=ANY($1)`, ids, state)
		if err != nil {
			return fid, errors.Join(cause, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fid, errors.Join(cause, err)
		}
		return fid, cause
	}
	if err = work.Commit(ctx); err != nil {
		return fid, err
	}
	// Only captured IDs are deleted; publications committed after our snapshot remain.
	return fid, tx.Commit(ctx)
}

// This worker also runs in synchronous mode so disabling async publication does
// not strand tasks created by an earlier process. It never locks a theme or user.
func (s *Store) RunForumStats(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	scheduled := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			job, cancel := context.WithTimeout(ctx, 10*time.Second)
			if time.Since(scheduled) >= time.Hour {
				if err := s.QueueAllForumStats(job); err != nil {
					logger.Error("forum statistics reconciliation enqueue failed")
				} else {
					scheduled = time.Now()
				}
			}
			start := time.Now()
			for batch := 0; batch < 10 && time.Since(start) < 250*time.Millisecond && job.Err() == nil; batch++ {
				if stat := s.pool.Stat(); stat.AcquiredConns()*4 >= stat.MaxConns()*3 {
					break
				}
				fid, err := s.ProcessForumStats(job)
				if err != nil {
					if ctx.Err() == nil {
						logger.Error("forum statistics reconciliation failed", "forumId", fid)
					}
					break
				}
				if fid == 0 {
					break
				}
			}
			cancel()
		}
	}
}
