// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"errors"
	"net/http"
	"time"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func (s *Server) titleError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrTitleConflict):
		s.fail(w, r, 409, "TITLE_CONFLICT", "配置版本已变化、幂等键冲突或主题已有采纳回复")
	case errors.Is(err, store.ErrTitleInvalid):
		s.fail(w, r, 422, "VALIDATION_FAILED", "称号配置或操作无效，请检查字段、状态、时间和条件")
	case errors.Is(err, store.ErrTitleForbidden):
		s.fail(w, r, 403, "FORBIDDEN", "无权执行此操作或称号不可佩戴")
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, r, 404, "NOT_FOUND", "内容不存在")
	default:
		s.log.Error("title operation failed", "err", err)
		s.fail(w, r, 500, "INTERNAL_ERROR", "操作失败，请稍后重试")
	}
	return true
}

func (s *Server) titlesGet(w http.ResponseWriter, r *http.Request) {
	defs, err := s.st.Titles(r.Context())
	if s.titleError(w, r, err) {
		return
	}
	out := []store.TitleDefinition{}
	for _, c := range defs {
		if c.Status != "active" && c.Status != "paused" {
			continue
		}
		visible := true
		for _, cond := range c.Conditions {
			if cond.ForumID > 0 && !canReadForum(r, cond.ForumID) {
				visible = false
			}
		}
		if visible {
			out = append(out, c)
		}
	}
	s.respond(w, 200, out)
}
func (s *Server) myTitles(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	v, err := s.st.UserTitles(r.Context(), User(r).ID)
	if !s.titleError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) equipTitle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TitleID *int64 `json:"titleId,string"`
	}
	if !s.membershipJSON(w, r, &req) || !s.checkNotBanned(w, r) {
		return
	}
	if req.TitleID == nil {
		s.titleError(w, r, store.ErrTitleInvalid)
		return
	}
	if !s.allow(r, "title-equip", 20, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "操作过于频繁")
		return
	}
	if s.titleError(w, r, s.st.EquipTitle(r.Context(), User(r).ID, *req.TitleID)) {
		return
	}
	s.respond(w, 200, map[string]bool{"saved": true})
}
func (s *Server) adminTitles(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.Titles(r.Context())
	if !s.titleError(w, r, err) {
		s.respond(w, 200, map[string]any{"titles": v, "metrics": store.TitleMetrics})
	}
}
func (s *Server) saveTitle(w http.ResponseWriter, r *http.Request) {
	var c store.TitleDefinition
	if !s.membershipJSON(w, r, &c) {
		return
	}
	if r.Method == http.MethodPost {
		if c.ID != 0 {
			s.titleError(w, r, store.ErrTitleInvalid)
			return
		}
	} else if c.ID != pathID(r, "titleId") || c.ID <= 0 {
		s.titleError(w, r, store.ErrTitleInvalid)
		return
	}
	v, err := s.st.SaveTitle(r.Context(), c, User(r).ID)
	if !s.titleError(w, r, err) {
		status := 200
		if r.Method == http.MethodPost {
			status = 201
		}
		s.respond(w, status, v)
	}
}
func (s *Server) previewTitle(w http.ResponseWriter, r *http.Request) {
	var c store.TitleDefinition
	if !s.membershipJSON(w, r, &c) {
		return
	}
	if !s.allow(r, "title-preview", 5, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "预览过于频繁")
		return
	}
	v, err := s.st.PreviewTitle(r.Context(), c)
	if !s.titleError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) adjustTitle(w http.ResponseWriter, r *http.Request) {
	var a store.TitleAdjustment
	if !s.membershipJSON(w, r, &a) {
		return
	}
	var pt perm.Point
	switch a.Action {
	case "grant":
		pt = perm.TitleGrant
	case "revoke":
		pt = perm.TitleRevoke
	default:
		s.titleError(w, r, store.ErrTitleInvalid)
		return
	}
	if !hasPoint(User(r), pt) {
		s.fail(w, r, 403, "FORBIDDEN", "缺少操作权限："+string(pt))
		return
	}
	err := s.st.AdjustTitle(r.Context(), pathID(r, "uid"), pathID(r, "titleId"), User(r).ID, a)
	if !s.titleError(w, r, err) {
		s.respond(w, 200, map[string]bool{"saved": true})
	}
}
func (s *Server) adminUserTitles(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.UserTitles(r.Context(), pathID(r, "uid"))
	if !s.titleError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) titleJobs(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	v, total, err := s.st.TitleJobs(r.Context(), pathID(r, "titleId"), page)
	if !s.titleError(w, r, err) {
		s.list(w, v, page, 30, total)
	}
}
func (s *Server) titleLogs(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	v, total, err := s.st.TitleLogs(r.Context(), pathID(r, "titleId"), page)
	if !s.titleError(w, r, err) {
		s.list(w, v, page, 30, total)
	}
}

func (s *Server) acceptReply(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !s.membershipJSON(w, r, &req) || !s.checkNotBanned(w, r) {
		return
	}
	p, th := s.visiblePost(w, r, pathID(r, "pid"))
	if p == nil {
		return
	}
	if !hasPoint(User(r), perm.ReplyAccept) {
		s.fail(w, r, 403, "FORBIDDEN", "无权采纳回复")
		return
	}
	if !s.allow(r, "reply-accept", 20, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "操作过于频繁")
		return
	}
	if s.titleError(w, r, s.st.SetAcceptedReply(r.Context(), th.ID, p.ID, User(r).ID, r.Method == http.MethodPut)) {
		return
	}
	s.respond(w, 200, map[string]bool{"accepted": r.Method == http.MethodPut})
}

func (s *Server) canAcceptReply(r *http.Request, p *store.Post, th *store.Thread) bool {
	u := User(r)
	m := membershipOf(r)
	return u != nil && m != nil && !m.Banned && !u.IsBlocked() && !u.MustChangePassword && hasPoint(u, perm.ReplyAccept) && u.ID == th.AuthorID && p.AuthorID != u.ID && p.Floor > 1 && !p.Pending && !th.Pending && !th.Closed && canReadForum(r, th.ForumID)
}

func (s *Server) titleRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/titles", s.titlesGet)
	m.HandleFunc("GET /api/v1/me/titles", s.myTitles)
	m.HandleFunc("PUT /api/v1/me/title", s.equipTitle)
	m.HandleFunc("PUT /api/v1/posts/{pid}/acceptance", s.acceptReply)
	m.HandleFunc("DELETE /api/v1/posts/{pid}/acceptance", s.acceptReply)
	for route, h := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/titles":                         s.adminTitles,
		"POST /api/v1/admin/titles":                        s.saveTitle,
		"PUT /api/v1/admin/titles/{titleId}":               s.saveTitle,
		"POST /api/v1/admin/titles/preview":                s.previewTitle,
		"GET /api/v1/admin/titles/{titleId}/jobs":          s.titleJobs,
		"GET /api/v1/admin/titles/{titleId}/logs":          s.titleLogs,
		"GET /api/v1/admin/users/{uid}/titles":             s.adminUserTitles,
		"PATCH /api/v1/admin/users/{uid}/titles/{titleId}": s.adjustTitle,
	} {
		m.HandleFunc(route, s.adminPointGuard(route, h))
	}
}
