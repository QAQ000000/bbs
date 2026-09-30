package api

import (
	"dzforum/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) followToggle(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	id := pathID(r, "id")
	if id <= 0 || id == u.ID {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效关注对象")
		return
	}
	var e error
	on := r.Method == http.MethodPost
	if !s.checkNotBanned(w, r) || !s.checkMustChangePassword(w, r) {
		return
	}
	if !s.allowKey("follow:"+idString(u.ID), 30, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "操作过于频繁")
		return
	}
	if on {
		e = s.st.Follow(r.Context(), u.ID, id)
	} else {
		e = s.st.Unfollow(r.Context(), u.ID, id)
	}
	if s.communityError(w, r, e) {
		return
	}
	s.respond(w, 200, map[string]any{"following": on, "userId": strconv.FormatInt(id, 10)})
}
func (s *Server) threadSubscribe(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	kind, id := "thread", pathID(r, "tid")
	if strings.Contains(r.Pattern, "/forums/") {
		kind, id = "forum", pathID(r, "fid")
	}
	if strings.Contains(r.Pattern, "/tags/") {
		kind, id = "tag", pathID(r, "tagId")
	}
	if id <= 0 {
		s.communityError(w, r, store.ErrCommunityInvalid)
		return
	}
	if r.Method == http.MethodDelete {
		if !s.communityError(w, r, s.st.DeleteSubscription(r.Context(), u.ID, id, kind)) {
			s.respond(w, 200, map[string]bool{"subscribed": false})
		}
		return
	}
	if !s.checkNotBanned(w, r) || !s.checkMustChangePassword(w, r) {
		return
	}
	switch kind {
	case "thread":
		th := s.visibleThread(w, r, id)
		if th == nil {
			return
		}
		if th.Pending {
			s.readError(w, r, store.ErrNotFound)
			return
		}
	case "forum":
		if _, err := s.st.Forum(r.Context(), id); s.readError(w, r, err) {
			return
		}
	case "tag":
		v, err := s.st.Tag(r.Context(), id)
		if s.readError(w, r, err) {
			return
		}
		if v.Status != "active" {
			s.communityError(w, r, store.ErrCommunityInvalid)
			return
		}
	}
	v := store.Subscription{Kind: kind, TargetID: id}
	if r.Method == http.MethodPut {
		fields := []string{"enabled", "notifyInApp", "notifyEmail"}
		values := []*bool{&v.Enabled, &v.NotifyInApp, &v.NotifyEmail}
		for i, key := range fields {
			switch r.PostFormValue(key) {
			case "true", "1":
				*values[i] = true
			case "false", "0":
				*values[i] = false
			default:
				s.communityError(w, r, store.ErrCommunityInvalid)
				return
			}
		}
		if raw := r.PostFormValue("mutedUntil"); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if s.communityError(w, r, invalidTime(err)) {
				return
			}
			v.MutedUntil = &t
		}
	}
	v, err := s.st.SaveSubscription(r.Context(), u.ID, v, r.Method == http.MethodPost)
	if !s.communityError(w, r, err) {
		s.respond(w, 200, v)
	}
}

func invalidTime(err error) error {
	if err != nil {
		return store.ErrCommunityInvalid
	}
	return nil
}

func (s *Server) subscriptionsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "thread"
	}
	page := pageOf(r)
	rows, total, err := s.st.Subscriptions(r.Context(), User(r).ID, kind, page)
	if !s.communityError(w, r, err) {
		s.list(w, rows, page, 30, total)
	}
}

func (s *Server) followsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	uid := User(r).ID
	followers := strings.HasSuffix(r.URL.Path, "/followers")
	if r.PathValue("id") != "" && pathID(r, "id") <= 0 {
		s.readError(w, r, store.ErrNotFound)
		return
	}
	if id := pathID(r, "id"); id > 0 {
		uid = id
		if _, err := s.st.UserByID(r.Context(), id); s.readError(w, r, err) {
			return
		}
	}
	page := pageOf(r)
	rows, total, err := s.st.FollowUsers(r.Context(), uid, followers, page)
	if s.readError(w, r, err) {
		return
	}
	// 每行的关注状态由后端显式返回，避免前端只查第一页导致误判。
	following := map[int64]bool{}
	ids := make([]int64, 0, len(rows))
	for _, v := range rows {
		ids = append(ids, v.ID)
	}
	if viewer := User(r); viewer != nil {
		if m, e := s.st.FollowingIDs(r.Context(), viewer.ID, ids); e == nil {
			following = m
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, v := range rows {
		out = append(out, map[string]any{
			"id":         idString(v.ID),
			"username":   v.Username,
			"followedAt": v.CreatedAt,
			"following":  following[v.ID],
		})
	}
	s.list(w, out, page, 30, total)
}
