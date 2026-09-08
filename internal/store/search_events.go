package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type SearchIndexQueueStatus struct {
	Pending          int64   `json:"pending"`
	Retrying         int64   `json:"retrying"`
	OldestAgeSeconds float64 `json:"oldestAgeSeconds"`
}

func (s *Store) SearchIndexQueueStatus(ctx context.Context) (SearchIndexQueueStatus, error) {
	var v SearchIndexQueueStatus
	err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE attempts>0),coalesce(extract(epoch FROM now()-min(created_at)),0)::float8 FROM search_index_events`).Scan(&v.Pending, &v.Retrying, &v.OldestAgeSeconds)
	return v, err
}

// QueueSearchIndex schedules an idempotent rebuild for one post.
func (s *Store) QueueSearchIndex(ctx context.Context, postID int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO search_index_events(post_id) VALUES($1) ON CONFLICT(post_id) DO UPDATE SET created_at=now(),next_attempt_at=now()`, postID)
	return err
}

func queueSearchIndexTx(ctx context.Context, tx pgx.Tx, postID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO search_index_events(post_id) VALUES($1) ON CONFLICT(post_id) DO UPDATE SET created_at=now(),next_attempt_at=now()`, postID)
	return err
}

// RunSearchIndex processes durable search rebuild events.
func (s *Store) RunSearchIndex(ctx context.Context, logger *slog.Logger) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			job, cancel := context.WithTimeout(ctx, 10*time.Second)
			start := time.Now()
			for i := 0; i < 10 && time.Since(start) < 250*time.Millisecond; i++ {
				processed, err := s.processSearchEvent(job)
				if err != nil {
					if ctx.Err() == nil {
						logger.Warn("搜索索引任务失败", "err", err)
					}
					break
				}
				if !processed {
					break
				}
			}
			cancel()
		}
	}
}

// ProcessSearchIndex claims and processes one due event. It commits retry
// metadata when indexing fails so a transient database error survives restart.
func (s *Store) ProcessSearchIndex(ctx context.Context) error {
	_, err := s.processSearchEvent(ctx)
	return err
}

func (s *Store) processSearchEvent(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id, postID, tid int64
	// Writers lock thread, then post, then enqueue. Match that order to avoid
	// deadlocks and hold the content stable until index and event commit together.
	err = tx.QueryRow(ctx, `SELECT t.id FROM search_index_events e JOIN posts p ON p.id=e.post_id
JOIN threads t ON t.id=p.thread_id WHERE e.next_attempt_at<=now() ORDER BY e.id LIMIT 1
FOR NO KEY UPDATE OF t SKIP LOCKED`).Scan(&tid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = tx.QueryRow(ctx, `SELECT p.id FROM posts p JOIN search_index_events e ON e.post_id=p.id
WHERE p.thread_id=$1 AND e.next_attempt_at<=now() ORDER BY e.id LIMIT 1 FOR NO KEY UPDATE OF p SKIP LOCKED`, tid).Scan(&postID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = tx.QueryRow(ctx, `SELECT id FROM search_index_events WHERE post_id=$1 AND next_attempt_at<=now() FOR UPDATE SKIP LOCKED`, postID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var title, body string
	var deleted bool
	work, workErr := tx.Begin(ctx)
	if workErr != nil {
		return false, workErr
	}
	err = work.QueryRow(ctx, `SELECT CASE WHEN p.floor=1 THEN t.title ELSE '' END,p.content_md,p.deleted OR t.deleted OR p.pending OR t.pending FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=$1`, postID).Scan(&title, &body, &deleted)
	if err == nil {
		if deleted {
			_, err = work.Exec(ctx, `UPDATE posts SET search_data=NULL WHERE id=$1`, postID)
		} else {
			_, err = work.Exec(ctx, `UPDATE posts SET search_data=setweight(to_tsvector('simple',$2),'A') || setweight(to_tsvector('simple',$3),'B') WHERE id=$1`, postID, SearchTokens(title), SearchTokens(body))
		}
	}
	if err == nil {
		err = work.Commit(ctx)
	} else {
		if rollbackErr := work.Rollback(ctx); rollbackErr != nil {
			return false, errors.Join(err, rollbackErr)
		}
	}
	if err != nil {
		state := ""
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			state = pgErr.Code
		}
		if _, updateErr := tx.Exec(ctx, `UPDATE search_index_events SET attempts=least(attempts+1,16),next_attempt_at=now()+least(300,power(2,attempts))*interval '1 second',last_sqlstate=$2 WHERE id=$1`, id, state); updateErr != nil {
			return false, errors.Join(err, updateErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, errors.Join(err, commitErr)
		}
		return false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM search_index_events WHERE id=$1`, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
