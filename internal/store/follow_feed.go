package store

import (
	"context"
	"errors"
	"time"
)

// FeedCursor 关注流游标载荷。签名与归属校验在 API 层完成，
// store 只复核当前账号、流类型与排序仍然一致，避免跨流 / 跨账号复用。
type FeedCursor struct {
	UserID    int64
	Stream    string
	Sort      string
	CreatedAt time.Time
	ID        int64
}

const feedSortCreated = "created"

var errFeedCursor = errors.New("invalid feed cursor")

// ErrFeedCursorInvalid 由 store 暴露给 API 层映射为 422。
var ErrFeedCursorInvalid = errFeedCursor

func validFeedCursor(c *FeedCursor, uid int64, stream string) bool {
	return c != nil && c.UserID == uid && c.Stream == stream && c.Sort == feedSortCreated && c.ID > 0 && !c.CreatedAt.IsZero()
}

func (s *Store) FollowingForumsFeed(ctx context.Context, uid int64, size int, after *FeedCursor) ([]*Thread, bool, error) {
	return s.followFeed(ctx, uid, size, after, "forums")
}

func (s *Store) FollowingUsersFeed(ctx context.Context, uid int64, size int, after *FeedCursor) ([]*Thread, bool, error) {
	return s.followFeed(ctx, uid, size, after, "users")
}

// followFeed 两类关注流共用同一查询形态：可见性、待审与删除都在 DB 层过滤，
// 按主题 created_at 与 id 倒序游标翻页。版块流只看订阅关系，
// 通知关闭或静音（enabled / notify_* / muted_until）不影响是否收录。
func (s *Store) followFeed(ctx context.Context, uid int64, size int, after *FeedCursor, stream string) ([]*Thread, bool, error) {
	if uid <= 0 || size < 1 || size > 100 {
		return nil, false, errors.New("invalid feed bounds")
	}
	if stream != "forums" && stream != "users" {
		return nil, false, ErrCommunityInvalid
	}
	if after != nil && !validFeedCursor(after, uid, stream) {
		return nil, false, errFeedCursor
	}
	var source string
	if stream == "forums" {
		source = "t.forum_id IN (SELECT forum_id FROM forum_subscriptions WHERE uid=$1)"
	} else {
		// 只收录关注用户作为主题作者发布的主题，不把回复当作新主题。
		source = "t.author_id IN (SELECT following_id FROM user_follows WHERE follower_id=$1)"
	}
	filter := " WHERE NOT t.deleted AND NOT t.pending AND " + source + forumFilter(ctx, "t.forum_id")
	args := []any{uid, size + 1}
	if after != nil {
		filter += " AND (t.created_at,t.id)<($3,$4)"
		args = append(args, after.CreatedAt, after.ID)
	}
	rows, err := s.pool.Query(ctx, "SELECT "+threadCols+" "+threadJoins+filter+
		" ORDER BY t.created_at DESC,t.id DESC LIMIT $2", args...)
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

func (s *Store) FollowingForumCount(ctx context.Context, uid int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM forum_subscriptions WHERE uid=$1", uid).Scan(&n)
	return n, err
}

func (s *Store) FollowingUserCount(ctx context.Context, uid int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM user_follows WHERE follower_id=$1", uid).Scan(&n)
	return n, err
}
