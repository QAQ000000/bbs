package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
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
			for i := 0; i < 10; i++ {
				if err := s.ProcessSearchIndex(ctx); err != nil {
					logger.Warn("搜索索引任务失败", "err", err)
					break
				}
			}
		}
	}
}

// ProcessSearchIndex claims and processes one due event. It commits retry
// metadata when indexing fails so a transient database error survives restart.
func (s *Store) ProcessSearchIndex(ctx context.Context) error {
	return s.processSearchEvent(ctx)
}

func (s *Store) processSearchEvent(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, postID int64
	err = tx.QueryRow(ctx, `SELECT id,post_id FROM search_index_events WHERE next_attempt_at<=now() ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &postID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var title, body string
	var deleted bool
	work, workErr := tx.Begin(ctx)
	if workErr != nil {
		return workErr
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
		_ = work.Rollback(ctx)
	}
	if err != nil {
		if _, updateErr := tx.Exec(ctx, `UPDATE search_index_events SET attempts=least(attempts+1,16),next_attempt_at=now()+make_interval(secs=>least(300,power(2,attempts))),last_sqlstate='' WHERE id=$1`, id); updateErr != nil {
			return errors.Join(err, updateErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return errors.Join(err, commitErr)
		}
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM search_index_events WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
