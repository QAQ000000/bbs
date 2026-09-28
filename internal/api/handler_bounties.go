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
	rows, err := s.st.ActiveBounties(r.Context(), before)
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
