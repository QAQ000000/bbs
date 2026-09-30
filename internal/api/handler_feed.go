package api

import (
	"errors"
	"net/http"
	"strconv"

	"dzforum/internal/store"
)

const followFeedMaxSize = 50

// feedForums / feedUsers 是登录用户的关注聚合流：
// 版块流按订阅关系，人的流按“主题作者被关注”，都在 DB 层过滤与游标翻页。
func (s *Server) feedForums(w http.ResponseWriter, r *http.Request) { s.followFeed(w, r, "forums") }
func (s *Server) feedUsers(w http.ResponseWriter, r *http.Request)  { s.followFeed(w, r, "users") }

func (s *Server) followFeed(w http.ResponseWriter, r *http.Request, stream string) {
	if !s.requireLogin(w, r) {
		return
	}
	uid := User(r).ID
	size := s.sets(r).ThreadsPerPage
	if size < 1 || size > followFeedMaxSize {
		size = 20
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > followFeedMaxSize {
			s.fail(w, r, 422, "VALIDATION_FAILED", "limit 需为 1 到 50 的整数")
			return
		}
		size = n
	}
	var after *store.FeedCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, ok := s.decodeFeedCursor(raw, uid, stream)
		if !ok {
			s.fail(w, r, 422, "INVALID_CURSOR", "游标无效，或不属于当前账号与该信息流")
			return
		}
		after = cursor
	}
	var rows []*store.Thread
	var more bool
	var following int
	var err error
	if stream == "forums" {
		rows, more, err = s.st.FollowingForumsFeed(r.Context(), uid, size, after)
		if err == nil {
			following, err = s.st.FollowingForumCount(r.Context(), uid)
		}
	} else {
		rows, more, err = s.st.FollowingUsersFeed(r.Context(), uid, size, after)
		if err == nil {
			following, err = s.st.FollowingUserCount(r.Context(), uid)
		}
	}
	if errors.Is(err, store.ErrFeedCursorInvalid) {
		s.fail(w, r, 422, "INVALID_CURSOR", "游标无效，或不属于当前账号与该信息流")
		return
	}
	if s.readError(w, r, err) {
		return
	}
	threads, err := s.memberThreadRows(r, rows)
	if s.readError(w, r, err) {
		return
	}
	next := ""
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = s.encodeFeedCursor(uid, stream, last.CreatedAt, last.ID)
	}
	writeJSON(w, 200, map[string]any{
		"data": map[string]any{"threads": threads},
		"meta": map[string]any{
			"pagination":     "cursor",
			"stream":         stream,
			"pageSize":       size,
			"hasMore":        more,
			"nextCursor":     next,
			"followingCount": following,
		},
	})
}
