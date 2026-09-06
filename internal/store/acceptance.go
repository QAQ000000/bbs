// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// The thread owner can accept one other user's visible reply. PUT is idempotent;
// replacing a different accepted reply requires an explicit cancellation first.
func (s *Store) SetAcceptedReply(ctx context.Context, tid, pid, actor int64, accept bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var owner int64
	var closed, hidden bool
	err = tx.QueryRow(ctx, `SELECT author_id,closed,pending OR deleted FROM threads WHERE id=$1 FOR UPDATE`, tid).Scan(&owner, &closed, &hidden)
	if errors.Is(err, pgx.ErrNoRows) || hidden {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != actor || closed {
		return ErrTitleForbidden
	}
	var restricted bool
	if err = tx.QueryRow(ctx, `SELECT coalesce(banned_until>now(),false) OR coalesce(blocked_until>now(),false) OR must_change_password FROM users WHERE id=$1`, actor).Scan(&restricted); err != nil {
		return err
	}
	if restricted {
		return ErrTitleForbidden
	}
	var author int64
	var floor int
	err = tx.QueryRow(ctx, `SELECT author_id,floor,pending OR deleted FROM posts WHERE id=$1 AND thread_id=$2 FOR UPDATE`, pid, tid).Scan(&author, &floor, &hidden)
	if errors.Is(err, pgx.ErrNoRows) || (accept && hidden) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if floor <= 1 || author == actor {
		return ErrTitleForbidden
	}
	var old int64
	err = tx.QueryRow(ctx, `SELECT post_id FROM accepted_replies WHERE thread_id=$1`, tid).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if accept {
		if old == pid {
			return nil
		}
		if old != 0 {
			return ErrTitleConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO accepted_replies(thread_id,post_id) VALUES($1,$2)`, tid, pid)
	} else {
		if old == 0 {
			return nil
		}
		if old != pid {
			return ErrTitleConflict
		}
		_, err = tx.Exec(ctx, `DELETE FROM accepted_replies WHERE thread_id=$1`, tid)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO acceptance_logs(thread_id,post_id,actor_id,accepted) VALUES($1,$2,$3,$4)`, tid, pid, actor, accept); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AcceptedReply(ctx context.Context, tid int64) (int64, error) {
	var pid int64
	err := s.pool.QueryRow(ctx, `SELECT a.post_id FROM accepted_replies a JOIN posts p ON p.id=a.post_id JOIN threads t ON t.id=a.thread_id WHERE a.thread_id=$1 AND NOT p.deleted AND NOT p.pending AND NOT t.deleted AND NOT t.pending`, tid).Scan(&pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return pid, err
}
