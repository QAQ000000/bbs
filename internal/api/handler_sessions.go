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

// adminUserSessions 管理员查看目标用户的设备会话（只读，含状态与脱敏 IP）。
func (s *Server) adminUserSessions(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r, "uid")
	if uid < 1 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效用户编号")
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
	// 先确认用户存在，区分“用户不存在”与“该用户没有会话”。
	if _, err := s.st.UserByID(r.Context(), uid); s.readError(w, r, err) {
		return
	}
	rows, err := s.st.DeviceSessions(r.Context(), uid, 0, before)
	if s.readError(w, r, err) {
		return
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = idString(rows[49].ID)
	}
	s.respond(w, 200, map[string]any{"items": rows, "nextBefore": next, "userId": idString(uid)})
}

// parseAdminSessionTarget 区分“全部会话”（路由没有 sessionId 通配符）与“单个会话”。
// 单个会话必须是正整数：解析失败只判为无效输入，绝不退化成撤销全部。
func parseAdminSessionTarget(raw string) (target int64, all bool, valid bool) {
	if raw == "" {
		return 0, true, true
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < 1 {
		return 0, false, false
	}
	return parsed, false, true
}

// adminUserSessionsManage 管理员撤销目标用户的单个会话（带 sessionId）或全部会话。
// 用路由是否带 {sessionId} 通配符来区分两种操作：单个路由必须给出正整数，
// 解析失败只返回 422，绝不退化成“撤销全部”。
func (s *Server) adminUserSessionsManage(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r, "uid")
	if uid < 1 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效用户编号")
		return
	}
	target, all, valid := parseAdminSessionTarget(r.PathValue("sessionId"))
	if !valid {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效设备编号")
		return
	}
	action := "all"
	detail := "用户 #" + idString(uid) + " 全部会话"
	if !all {
		action = "revoke"
		detail = "用户 #" + idString(uid) + " 会话 #" + idString(target)
	}
	n, err := s.st.AdminRevokeSessions(r.Context(), uid, target, action)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, 404, "NOT_FOUND", "用户或会话不存在")
		return
	}
	if err != nil {
		s.fail(w, r, 503, "SESSION_UPDATE_FAILED", "会话撤销失败，请稍后重试")
		return
	}
	s.logOp(r, "user.sessions."+action, detail+"，撤销 "+strconv.FormatInt(n, 10)+" 个")
	s.respond(w, 200, map[string]any{"affected": n, "action": action})
}
