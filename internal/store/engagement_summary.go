package store

import (
	"context"
	"time"
)

// Small, explicit public projections. Operational refund diagnostics and poll
// options remain in their dedicated APIs, never in list metadata.
type PollSummary struct {
	Question   string    `json:"question"`
	State      string    `json:"state"`
	MaxChoices int       `json:"maxChoices"`
	Voters     int64     `json:"voters"`
	ClosesAt   time.Time `json:"closesAt"`
	Closed     bool      `json:"closed"`
	HasVoted   bool      `json:"hasVoted"`
}
type BountySummary struct {
	Amount   int64     `json:"amount"`
	State    string    `json:"state"`
	ClosesAt time.Time `json:"closesAt"`
	Expired  bool      `json:"expired"`
	PostID   int64     `json:"postId,string"`
}
type ThreadEngagement struct {
	Poll                                    *PollSummary
	Bounty                                  *BountySummary
	HasPoll, HasAccepted, HasVisibleReplies bool
	AcceptedPostID                          int64
}

// Caller has already authorized topic visibility. One query for the whole page;
// pending/rejected poll text is selected only for its author or a poll manager.
func (s *Store) ThreadEngagements(ctx context.Context, tids []int64, uid int64, pollManager bool) (map[int64]ThreadEngagement, error) {
	out := map[int64]ThreadEngagement{}
	if len(tids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id,p.thread_id IS NOT NULL,
 CASE WHEN p.state IN ('published','closed') OR t.author_id=$2 OR $3 THEN p.question ELSE NULL END,
 coalesce(p.state,''),coalesce(p.max_choices,0),coalesce(p.voters,0),coalesce(p.closes_at,t.created_at),
 coalesce(p.state<>'published' OR p.closes_at<=statement_timestamp() OR t.closed OR t.pending OR t.deleted,true),
 pb.user_id IS NOT NULL,b.thread_id IS NOT NULL,coalesce(b.amount,0),coalesce(b.state,''),coalesce(b.closes_at,t.created_at),
 coalesce(b.closes_at<=statement_timestamp(),false),coalesce(b.post_id,0),a.post_id IS NOT NULL,
 CASE WHEN NOT ap.deleted AND NOT ap.pending AND NOT t.pending AND NOT t.deleted THEN a.post_id ELSE 0 END,
 t.author_id=$2 AND EXISTS(SELECT 1 FROM posts rp WHERE rp.thread_id=t.id AND rp.floor>1 AND NOT rp.deleted AND NOT rp.pending)
 FROM threads t LEFT JOIN thread_polls p ON p.thread_id=t.id
 LEFT JOIN poll_ballots pb ON pb.thread_id=t.id AND pb.user_id=$2
 LEFT JOIN thread_bounties b ON b.thread_id=t.id
 LEFT JOIN accepted_replies a ON a.thread_id=t.id LEFT JOIN posts ap ON ap.id=a.post_id
 WHERE t.id=ANY($1)`, tids, uid, pollManager)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid int64
		var v ThreadEngagement
		var p PollSummary
		var b BountySummary
		var question *string
		var hasBounty bool
		if err = rows.Scan(&tid, &v.HasPoll, &question, &p.State, &p.MaxChoices, &p.Voters, &p.ClosesAt, &p.Closed, &p.HasVoted, &hasBounty, &b.Amount, &b.State, &b.ClosesAt, &b.Expired, &b.PostID, &v.HasAccepted, &v.AcceptedPostID, &v.HasVisibleReplies); err != nil {
			return nil, err
		}
		if question != nil {
			p.Question = *question
			v.Poll = &p
		}
		if hasBounty {
			v.Bounty = &b
		}
		out[tid] = v
	}
	return out, rows.Err()
}
