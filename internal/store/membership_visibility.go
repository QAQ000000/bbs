// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"strconv"
	"strings"
)

type visibleForumsKey struct{}

// WithVisibleForums applies the caller's access scope before SQL pagination.
// A missing scope is reserved for internal/admin operations; an empty slice denies all.
func WithVisibleForums(ctx context.Context, ids []int64) context.Context {
	return context.WithValue(ctx, visibleForumsKey{}, append([]int64{}, ids...))
}
func forumFilter(ctx context.Context, column string) string {
	ids, ok := ctx.Value(visibleForumsKey{}).([]int64)
	if !ok {
		return ""
	}
	if len(ids) == 0 {
		return " AND false"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return " AND " + column + " IN (" + strings.Join(parts, ",") + ")"
}
func (s *Store) MemberForums(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM forums ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) UploadForum(ctx context.Context, pid int64, fid *int64) error {
	return s.pool.QueryRow(ctx, `SELECT t.forum_id FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=$1`, pid).Scan(fid)
}
