package store

import (
	"context"
	"errors"
	"time"
)

type ThreadCursor struct {
	ForumID    int64     `json:"forumId"`
	ID         int64     `json:"id"`
	LastPostAt time.Time `json:"lastPostAt"`
}

// ThreadFeed uses a seek boundary and one lookahead row, without an exact count.
func (s *Store) ThreadFeed(ctx context.Context, forumID int64, size int, after *ThreadCursor) ([]*Thread, bool, error) {
	if size < 1 || size > 100 || forumID < 0 {
		return nil, false, errors.New("invalid feed bounds")
	}
	filter := ` WHERE NOT t.deleted AND NOT t.pending` + forumFilter(ctx, "t.forum_id")
	args := []any{size + 1}
	if forumID > 0 {
		args = append(args, forumID)
		filter += ` AND t.forum_id=$2 AND t.sticky=0`
	}
	if after != nil {
		if after.ID <= 0 || after.LastPostAt.IsZero() || after.ForumID != forumID {
			return nil, false, errors.New("invalid feed cursor")
		}
		// Parameter positions differ for global and per-forum feeds.
		if forumID > 0 {
			filter += ` AND (t.last_post_at,t.id)<($3,$4)`
		} else {
			filter += ` AND (t.last_post_at,t.id)<($2,$3)`
		}
		args = append(args, after.LastPostAt, after.ID)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+threadCols+` `+threadJoins+filter+`
	 ORDER BY t.last_post_at DESC,t.id DESC LIMIT $1`, args...)
	list, err := collectThreads(rows, err)
	if err != nil {
		return nil, false, err
	}
	more := len(list) > size
	if more {
		list = list[:size]
	}
	return list, more, nil
}
