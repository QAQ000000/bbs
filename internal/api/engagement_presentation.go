package api

import (
	"dzforum/internal/perm"
	"dzforum/internal/store"
	"net/http"
)

// Explicit public projection so future admin-only settings cannot leak by
// adding fields to the persisted administrative configuration.
func (s *Server) engagementRules(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.EngagementConfig(r.Context())
	if s.engagementError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{
		"poll":    map[string]any{"enabled": c.Poll.Enabled, "maxOptions": c.Poll.MaxOptions, "maxDays": c.Poll.MaxDays},
		"bounty":  map[string]any{"enabled": c.Bounty.Enabled, "minPoints": c.Bounty.MinPoints, "maxPoints": c.Bounty.MaxPoints, "maxDays": c.Bounty.MaxDays},
		"checkin": map[string]any{"enabled": c.Checkin.Enabled, "experience": c.Checkin.Experience, "points": c.Checkin.Points, "timeZone": c.Checkin.TimeZone},
	})
}
func (s *Server) engagementMetadata(r *http.Request, tids []int64) (map[int64]store.ThreadEngagement, error) {
	uid := int64(0)
	if u := User(r); u != nil {
		uid = u.ID
	}
	return s.st.ThreadEngagements(r.Context(), tids, uid, hasPoint(User(r), perm.AdminPanel) && hasPoint(User(r), perm.PollManage))
}
func (s *Server) threadEngagementResponse(r *http.Request, th *store.Thread, m map[string]any, v store.ThreadEngagement, c store.EngagementConfig) {
	m["poll"], m["bounty"] = v.Poll, v.Bounty
	m["acceptedPostId"] = idString(v.AcceptedPostID)
	u, member := User(r), membershipOf(r)
	active := u != nil && member != nil && !member.Banned && !u.IsBlocked() && !u.MustChangePassword && s.canViewThread(r, th)
	owner := active && u.ID == th.AuthorID
	open := !th.Closed && !th.Pending
	p, b := v.Poll, v.Bounty
	m["capabilities"] = map[string]bool{
		"canModerate":     s.canModerateThread(r, th),
		"canReply":        s.memberDecision(r, "post.reply", th.ForumID, nil, th).Allowed,
		"canCreatePoll":   owner && open && !v.HasPoll && c.Poll.Enabled && s.memberDecision(r, "poll.create", th.ForumID, nil, th).Allowed,
		"canVote":         active && open && p != nil && !p.Closed && !p.HasVoted && c.Poll.Enabled && s.memberDecision(r, "poll.vote", th.ForumID, nil, th).Allowed,
		"canClosePoll":    owner && p != nil && (p.State == "pending" || p.State == "published"),
		"canCreateBounty": owner && open && b == nil && !v.HasAccepted && c.Bounty.Enabled && s.memberDecision(r, "bounty.create", th.ForumID, nil, th).Allowed,
		"canCancelBounty": owner && b != nil && b.State == "active" && (b.Expired || !v.HasVisibleReplies),
	}
}
func (s *Server) acceptanceCapabilities(r *http.Request, p *store.Post, th *store.Thread, caps map[string]bool, v store.ThreadEngagement) {
	allowed := s.canAcceptReply(r, p, th)
	b := v.Bounty
	caps["canAccept"] = allowed && !v.HasAccepted && (b == nil || b.State == "canceled" || b.State == "expired" || b.State == "active" && !b.Expired)
	caps["canUnaccept"] = allowed && v.AcceptedPostID == p.ID && (b == nil || b.State != "awarded")
}
