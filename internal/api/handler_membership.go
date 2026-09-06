// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func (s *Server) membershipJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !s.requireLogin(w, r) || !s.checkMustChangePassword(w, r) {
		return false
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "CSRF_INVALID", "CSRF token 无效")
		return false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		s.fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "请使用 application/json")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		s.fail(w, r, 422, "VALIDATION_FAILED", "JSON 字段、类型或大小无效")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		s.fail(w, r, 422, "VALIDATION_FAILED", "只能提交一个 JSON 对象")
		return false
	}
	return true
}
func (s *Server) membershipLevels(w http.ResponseWriter, r *http.Request) {
	s.respond(w, 200, membershipOf(r).Config.Levels)
}
func (s *Server) membershipMe(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	m := membershipOf(r)
	s.respond(w, 200, map[string]any{"membership": m.Member, "usage": m.Quota, "effectiveLimits": s.effectiveMemberLimits(r), "configVersion": m.Config.Version})
}
func (s *Server) membershipExperience(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	page := pageOf(r)
	v, total, err := s.st.MemberExperience(r.Context(), User(r).ID, page)
	if !s.readError(w, r, err) {
		s.list(w, v, page, 30, total)
	}
}
func (s *Server) membershipActivity(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) || !s.checkNotBanned(w, r) || !s.checkMustChangePassword(w, r) {
		return
	}
	if err := s.st.RecordMemberActivity(r.Context(), User(r).ID); s.readError(w, r, err) {
		return
	}
	if _, err := s.st.ProcessMemberEvents(r.Context(), 100); s.readError(w, r, err) {
		return
	}
	if err := s.st.UpgradeMember(r.Context(), User(r).ID); s.readError(w, r, err) {
		return
	}
	v, err := s.st.Membership(r.Context(), User(r).ID)
	if !s.readError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) adminMembership(w http.ResponseWriter, r *http.Request) {
	s.respond(w, 200, membershipOf(r).Config)
}
func (s *Server) adminMembershipPreview(w http.ResponseWriter, r *http.Request) {
	var c store.MembershipConfig
	if !s.membershipJSON(w, r, &c) {
		return
	}
	if err := c.Validate(); err != nil {
		s.fail(w, r, 422, "VALIDATION_FAILED", err.Error())
		return
	}
	v, err := s.st.PreviewMembership(r.Context(), c)
	if err != nil {
		s.membershipConfigError(w, r, err)
		return
	}
	s.respond(w, 200, v)
}
func (s *Server) adminMembershipSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Config       store.MembershipConfig `json:"config"`
		PreviewToken string                 `json:"previewToken"`
	}
	if !s.membershipJSON(w, r, &req) {
		return
	}
	if err := req.Config.Validate(); err != nil {
		s.fail(w, r, 422, "VALIDATION_FAILED", err.Error())
		return
	}
	if err := s.st.ApplyMembership(r.Context(), req.Config, req.PreviewToken, User(r).ID); err != nil {
		s.membershipConfigError(w, r, err)
		return
	}
	s.respond(w, 200, map[string]int64{"version": req.Config.Version + 1})
}
func (s *Server) membershipConfigError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.HasPrefix(err.Error(), "会员配置无效") || strings.HasPrefix(err.Error(), "unknown member level") || err.Error() == "配置引用的版块不存在" {
		s.fail(w, r, 422, "VALIDATION_FAILED", "等级被用户使用或配置引用无效，请先迁移用户并检查版块")
		return
	}
	s.memberError(w, r, err)
}
func (s *Server) adminMemberGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.Membership(r.Context(), pathID(r, "uid"))
	if !s.readError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) adminMemberExperience(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	v, total, err := s.st.MemberExperience(r.Context(), pathID(r, "uid"), page)
	if !s.readError(w, r, err) {
		s.list(w, v, page, 30, total)
	}
}
func (s *Server) adminMemberAdjust(w http.ResponseWriter, r *http.Request) {
	var a store.MemberAdjustment
	if !s.membershipJSON(w, r, &a) {
		return
	}
	if a.LevelID != nil || a.Locked != nil {
		if !hasPoint(User(r), perm.MemberAdjust) {
			s.fail(w, r, 403, "FORBIDDEN", "无权调整等级")
			return
		}
	}
	if a.Delta != 0 && !hasPoint(User(r), perm.ExperienceAdjust) {
		s.fail(w, r, 403, "FORBIDDEN", "无权调整经验")
		return
	}
	if (a.LevelID == nil && a.Locked == nil && a.Delta == 0) || strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 500 || len(a.Key) < 8 || len(a.Key) > 100 || a.Delta < -1e9 || a.Delta > 1e9 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "请提供有效原因、幂等键和分值")
		return
	}
	if a.LevelID != nil {
		if _, ok := membershipOf(r).Config.Level(*a.LevelID); !ok {
			s.fail(w, r, 422, "VALIDATION_FAILED", "等级不存在")
			return
		}
	}
	err := s.st.AdjustMember(r.Context(), pathID(r, "uid"), User(r).ID, a)
	if err != nil {
		if err.Error() == "调整后经验不能为负" {
			s.fail(w, r, 422, "VALIDATION_FAILED", err.Error())
		} else {
			s.memberError(w, r, err)
		}
		return
	}
	s.adminMemberGet(w, r)
}
func (s *Server) adminMemberLogs(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	v, total, err := s.st.MemberAudit(r.Context(), page)
	if !s.readError(w, r, err) {
		s.list(w, v, page, 30, total)
	}
}
func (s *Server) adminMemberDiagnose(w http.ResponseWriter, r *http.Request) {
	uid, err := parseMemberID(r.URL.Query().Get("userId"))
	if err != nil {
		s.fail(w, r, 422, "VALIDATION_FAILED", err.Error())
		return
	}
	u, err := s.st.UserByID(r.Context(), uid)
	if s.readError(w, r, err) {
		return
	}
	rr := requestWithUser(r, u)
	rr, err = s.loadMembership(rr)
	if s.readError(w, r, err) {
		return
	}
	fid := queryID(r, "forumId")
	var p *store.Post
	var th *store.Thread
	if pid := queryID(r, "postId"); pid > 0 {
		p, err = s.st.Post(r.Context(), pid)
		if s.readError(w, r, err) {
			return
		}
		th, err = s.st.Thread(r.Context(), p.ThreadID)
		if s.readError(w, r, err) {
			return
		}
		fid = th.ForumID
	} else if tid := queryID(r, "threadId"); tid > 0 {
		th, err = s.st.Thread(r.Context(), tid)
		if s.readError(w, r, err) {
			return
		}
		fid = th.ForumID
	}
	d := s.memberDecision(rr, r.URL.Query().Get("action"), fid, p, th)
	s.respond(w, 200, d)
}

func (s *Server) membershipRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/membership/levels", s.membershipLevels)
	m.HandleFunc("GET /api/v1/me/membership", s.membershipMe)
	m.HandleFunc("GET /api/v1/me/experience", s.membershipExperience)
	m.HandleFunc("POST /api/v1/me/activity", s.action(s.membershipActivity))
	for route, h := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/membership": s.adminMembership, "POST /api/v1/admin/membership/preview": s.adminMembershipPreview, "PUT /api/v1/admin/membership": s.adminMembershipSave,
		"GET /api/v1/admin/membership/users/{uid}": s.adminMemberGet, "PATCH /api/v1/admin/membership/users/{uid}": s.adminMemberAdjust, "GET /api/v1/admin/membership/users/{uid}/experience": s.adminMemberExperience,
		"GET /api/v1/admin/membership/logs": s.adminMemberLogs, "GET /api/v1/admin/membership/diagnose": s.adminMemberDiagnose,
	} {
		m.HandleFunc(route, s.adminPointGuard(route, h))
	}
}
