package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"dzforum/internal/store"
)

func (s *Server) bountyGet(w http.ResponseWriter, r *http.Request) {
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	b, err := s.st.Bounty(r.Context(), th.ID)
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, b)
	}
}
func (s *Server) bountyCreate(w http.ResponseWriter, r *http.Request) {
	var req store.BountyInput
	if !s.membershipJSON(w, r, &req) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil || !s.engagementMember(w, r, "bounty.create", th) {
		return
	}
	if s.engagementError(w, r, s.st.CreateBounty(r.Context(), th.ID, User(r).ID, req)) {
		return
	}
	s.bountyGet(w, r)
}
func (s *Server) bountyCancel(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !s.membershipJSON(w, r, &req) || !s.checkNotBanned(w, r) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	_, err := s.st.RefundBounty(r.Context(), th.ID, User(r).ID, false, false, "")
	if !s.engagementError(w, r, err) {
		s.bountyGet(w, r)
	}
}
func (s *Server) adminBountyCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !s.membershipJSON(w, r, &req) {
		return
	}
	tid := pathID(r, "tid")
	_, err := s.st.RefundBounty(r.Context(), tid, User(r).ID, true, false, req.Reason)
	if s.engagementError(w, r, err) {
		return
	}
	b, err := s.st.Bounty(r.Context(), tid)
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, b)
	}
}
func (s *Server) adminBounties(w http.ResponseWriter, r *http.Request) {
	var before int64
	var err error
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			s.engagementError(w, r, store.ErrEngagementInvalid)
			return
		}
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "active"
	}
	failed := false
	if raw := r.URL.Query().Get("refundFailed"); raw != "" {
		failed, err = strconv.ParseBool(raw)
		if err != nil {
			s.engagementError(w, r, store.ErrEngagementInvalid)
			return
		}
	}
	rows, err := s.st.AdminBounties(r.Context(), before, state, failed)
	if s.engagementError(w, r, err) {
		return
	}
	next := ""
	if len(rows) == 50 {
		next = idString(rows[len(rows)-1].ThreadID)
	}
	s.respond(w, 200, map[string]any{"items": rows, "nextBefore": next})
}
func (s *Server) runBountyRefunds(ctx context.Context) {
	runAnalyticsWorker(ctx, []analyticsJob{{name: "bounty-refunds", interval: time.Minute, refresh: func(ctx context.Context) error {
		_, err := s.st.ProcessBountyRefunds(ctx)
		return err
	}}}, func(name string, err error, retry time.Duration) {
		s.log.Warn("bounty refunds failed", "err", err, "retryIn", retry)
	})
}

func (s *Server) adminBountyGet(w http.ResponseWriter, r *http.Request) {
	b, err := s.st.AdminBounty(r.Context(), pathID(r, "tid"))
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, b)
	}
}
func (s *Server) adminBountyDiagnostics(w http.ResponseWriter, r *http.Request) {
	d, err := s.st.BountyRefundDiagnostics(r.Context())
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, d)
	}
}
func (s *Server) adminBountyRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !s.membershipJSON(w, r, &req) {
		return
	}
	if s.engagementError(w, r, s.st.RetryBountyRefund(r.Context(), pathID(r, "tid"), User(r).ID, req.Reason)) {
		return
	}
	b, err := s.st.AdminBounty(r.Context(), pathID(r, "tid"))
	if !s.engagementError(w, r, err) {
		s.respond(w, 202, b)
	}
}
