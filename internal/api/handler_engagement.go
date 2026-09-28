package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func (s *Server) engagementError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrEngagementInvalid):
		s.fail(w, r, 422, "ENGAGEMENT_INVALID", "参数或运营配置无效")
	case errors.Is(err, store.ErrEngagementConflict):
		s.fail(w, r, 409, "ENGAGEMENT_CONFLICT", "配置或业务状态已变化，不能重复或覆盖操作")
	case errors.Is(err, store.ErrEngagementForbidden):
		s.fail(w, r, 403, "FORBIDDEN", "无权执行此操作")
	case errors.Is(err, store.ErrEngagementClosed):
		s.fail(w, r, 409, "ENGAGEMENT_CLOSED", "功能已停用或活动已结束")
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, r, 404, "NOT_FOUND", "内容不存在或不可见")
	case errors.Is(err, store.ErrPointsInsufficient):
		s.fail(w, r, 409, "POINTS_INSUFFICIENT", "可用积分不足")
	default:
		s.log.Error("engagement operation failed", "err", err)
		s.fail(w, r, 503, "ENGAGEMENT_UNAVAILABLE", "服务暂不可用")
	}
	return true
}
func (s *Server) engagementMember(w http.ResponseWriter, r *http.Request, action string, th *store.Thread) bool {
	fid := int64(0)
	if th != nil {
		fid = th.ForumID
	}
	d := s.memberDecision(r, action, fid, nil, th)
	if !d.Allowed {
		s.fail(w, r, 403, "MEMBER_PERMISSION_DENIED", d.Reason)
		return false
	}
	if !s.allow(r, "engagement:"+action, 20, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "操作过于频繁")
		return false
	}
	return true
}
func (s *Server) engagementConfigGet(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.EngagementConfig(r.Context())
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, c)
	}
}
func (s *Server) engagementConfigSave(w http.ResponseWriter, r *http.Request) {
	var c store.EngagementConfig
	if !s.membershipJSON(w, r, &c) {
		return
	}
	if s.engagementError(w, r, s.st.SaveEngagementConfig(r.Context(), c, User(r).ID)) {
		return
	}
	c.Version++
	s.respond(w, 200, c)
}
func (s *Server) pollGet(w http.ResponseWriter, r *http.Request) {
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	uid := int64(0)
	if User(r) != nil {
		uid = User(r).ID
	}
	p, err := s.st.Poll(r.Context(), th.ID, uid)
	if s.engagementError(w, r, err) {
		return
	}
	if (p.State == "pending" || p.State == "rejected") && uid != th.AuthorID && !(hasPoint(User(r), perm.AdminPanel) && hasPoint(User(r), perm.PollManage)) {
		s.fail(w, r, 404, "NOT_FOUND", "投票不存在")
		return
	}
	s.respond(w, 200, p)
}
func (s *Server) pollCreate(w http.ResponseWriter, r *http.Request) {
	var req store.PollInput
	if !s.membershipJSON(w, r, &req) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil || !s.engagementMember(w, r, "poll.create", th) {
		return
	}
	if len(req.Options) > 20 {
		s.engagementError(w, r, store.ErrEngagementInvalid)
		return
	}
	texts := append([]string{req.Question}, req.Options...)
	texts = s.censorTexts(r, texts...)
	req.Question = texts[0]
	req.Options = texts[1:]
	pending, reason := s.moderationDecision(r, User(r), th.ForumID, strings.Join(texts, " "))
	if s.engagementError(w, r, s.st.CreatePoll(r.Context(), th.ID, User(r).ID, req, pending, reason)) {
		return
	}
	p, err := s.st.Poll(r.Context(), th.ID, User(r).ID)
	if !s.engagementError(w, r, err) {
		s.respond(w, 201, p)
	}
}
func (s *Server) pollVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Options []int32 `json:"optionIds"`
	}
	if !s.membershipJSON(w, r, &req) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil || !s.engagementMember(w, r, "poll.vote", th) {
		return
	}
	if s.engagementError(w, r, s.st.VotePoll(r.Context(), th.ID, User(r).ID, req.Options)) {
		return
	}
	s.pollGet(w, r)
}
func (s *Server) pollClose(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !s.membershipJSON(w, r, &req) || !s.checkNotBanned(w, r) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	if s.engagementError(w, r, s.st.ManagePoll(r.Context(), th.ID, User(r).ID, false, "close", "")) {
		return
	}
	s.pollGet(w, r)
}
func (s *Server) adminPollModerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if !s.membershipJSON(w, r, &req) {
		return
	}
	if s.engagementError(w, r, s.st.ManagePoll(r.Context(), pathID(r, "tid"), User(r).ID, true, req.Action, req.Reason)) {
		return
	}
	s.pollGet(w, r)
}
func (s *Server) adminPolls(w http.ResponseWriter, r *http.Request) {
	var before int64
	var err error
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			s.engagementError(w, r, store.ErrEngagementInvalid)
			return
		}
	}
	rows, err := s.st.PendingPolls(r.Context(), before)
	if s.engagementError(w, r, err) {
		return
	}
	next := ""
	if len(rows) == 50 {
		next = idString(rows[len(rows)-1].ThreadID)
	}
	s.respond(w, 200, map[string]any{"items": rows, "nextBefore": next})
}
func (s *Server) engagementRoutes() {
	s.mux.HandleFunc("GET /api/v1/threads/{tid}/bounty", s.bountyGet)
	s.mux.HandleFunc("POST /api/v1/threads/{tid}/bounty", s.bountyCreate)
	s.mux.HandleFunc("POST /api/v1/threads/{tid}/bounty/cancel", s.bountyCancel)
	s.mux.HandleFunc("GET /api/v1/threads/{tid}/poll", s.pollGet)
	s.mux.HandleFunc("POST /api/v1/threads/{tid}/poll", s.pollCreate)
	s.mux.HandleFunc("PUT /api/v1/threads/{tid}/poll/vote", s.pollVote)
	s.mux.HandleFunc("POST /api/v1/threads/{tid}/poll/close", s.pollClose)
	for route, h := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/bounties":                s.adminBounties,
		"POST /api/v1/admin/bounties/{tid}/cancel":  s.adminBountyCancel,
		"GET /api/v1/admin/engagement/config":       s.engagementConfigGet,
		"PUT /api/v1/admin/engagement/config":       s.engagementConfigSave,
		"GET /api/v1/admin/polls":                   s.adminPolls,
		"POST /api/v1/admin/polls/{tid}/moderation": s.adminPollModerate,
	} {
		s.mux.HandleFunc(route, s.adminPointGuard(route, h))
	}
}
