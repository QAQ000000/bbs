// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"dzforum/internal/store"
	"net/http"
)

func (s *Server) notificationSummary(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	count, err := s.st.NotificationUnreadCount(r.Context(), User(r).ID)
	if !s.readError(w, r, err) {
		s.respond(w, 200, map[string]any{"unread": count})
	}
}

func (s *Server) notificationPreferencesGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	prefs, err := s.st.NotificationPreferences(r.Context(), User(r).ID)
	if !s.readError(w, r, err) {
		s.respond(w, 200, prefs)
	}
}

func (s *Server) notificationPreferencesSave(w http.ResponseWriter, r *http.Request) {
	prefs := map[string]bool{}
	for _, key := range store.NotificationPreferenceKeys {
		value := r.PostFormValue(key)
		switch value {
		case "1", "true":
			prefs[key] = true
		case "0", "false":
			prefs[key] = false
		default:
			s.fail(w, r, 422, "VALIDATION_FAILED", "请提供全部通知偏好布尔值")
			return
		}
	}
	if !s.readError(w, r, s.st.SaveNotificationPreferences(r.Context(), User(r).ID, prefs)) {
		s.respond(w, 200, prefs)
	}
}

func (s *Server) ownContentGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	kind, status := r.URL.Query().Get("kind"), r.URL.Query().Get("status")
	if kind == "" {
		kind = "threads"
	}
	if status == "" {
		status = "all"
	}
	if kind != "threads" && kind != "replies" {
		s.fail(w, r, 422, "VALIDATION_FAILED", "kind 必须为 threads 或 replies")
		return
	}
	switch status {
	case "all", "published", "pending", "rejected", "deleted":
	default:
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效内容状态")
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.OwnContentPage(r.Context(), User(r).ID, kind, status, page, 30)
	if !s.readError(w, r, err) {
		s.list(w, rows, page, 30, total)
	}
}

func (s *Server) postPositionGet(w http.ResponseWriter, r *http.Request) {
	p, th := s.visiblePost(w, r, pathID(r, "pid"))
	if p == nil {
		return
	}
	uid := int64(0)
	if User(r) != nil {
		uid = User(r).ID
	}
	size := s.sets(r).PostsPerPage
	page, err := s.st.PostPosition(r.Context(), p, uid, s.canModerateThread(r, th), size)
	if !s.readError(w, r, err) {
		s.respond(w, 200, map[string]any{"postId": idString(p.ID), "threadId": idString(th.ID), "floor": p.Floor, "page": page, "pageSize": size})
	}
}
