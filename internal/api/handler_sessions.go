package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"dzforum/internal/store"
)

func (s *Server) sessionsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			s.fail(w, r, 422, "VALIDATION_FAILED", "无效分页游标")
			return
		}
	}
	rows, err := s.st.DeviceSessions(r.Context(), User(r).ID, Session(r).ID, before)
	if s.readError(w, r, err) {
		return
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = idString(rows[49].ID)
	}
	s.respond(w, 200, map[string]any{"items": rows, "nextBefore": next, "currentSessionId": idString(Session(r).ID)})
}

func (s *Server) sessionManage(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "FORBIDDEN", "表单已过期，请刷新重试")
		return
	}
	if !s.allow(r, "session-manage", 30, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "操作过于频繁")
		return
	}
	action := "revoke"
	target := pathID(r, "sessionId")
	switch {
	case r.Method == http.MethodPatch:
		action = "rename"
	case r.URL.Path == "/api/v1/me/sessions/revoke-others":
		action = "others"
	case r.URL.Path == "/api/v1/me/sessions":
		action = "all"
	}
	if (action == "rename" || action == "revoke") && target < 1 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效设备编号")
		return
	}
	n, err := s.st.ManageDevice(r.Context(), User(r).ID, Session(r).ID, target, action, r.PostFormValue("name"))
	if errors.Is(err, store.ErrDeviceName) {
		s.fail(w, r, 422, "VALIDATION_FAILED", "设备名称须为 1 至 80 个字符，不能包含控制字符")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, 404, "NOT_FOUND", "设备不存在或会话已失效")
		return
	}
	if err != nil {
		s.fail(w, r, 503, "SESSION_UPDATE_FAILED", "设备更新失败，请稍后重试")
		return
	}
	signedOut := action == "all" || (action == "revoke" && target == Session(r).ID)
	if signedOut {
		s.setSessionCookie(w, "", -1)
	}
	s.respond(w, 200, map[string]any{"affected": n, "signedOut": signedOut})
}
