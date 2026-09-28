package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PollInput struct {
	Question      string   `json:"question"`
	Options       []string `json:"options"`
	MaxChoices    int      `json:"maxChoices"`
	DurationHours int      `json:"durationHours"`
}
type PollOption struct {
	ID    int    `json:"id"`
	Text  string `json:"text"`
	Votes int64  `json:"votes"`
}
type Poll struct {
	ThreadID   int64        `json:"threadId,string"`
	Question   string       `json:"question"`
	MaxChoices int          `json:"maxChoices"`
	State      string       `json:"state"`
	ClosesAt   time.Time    `json:"closesAt"`
	CreatedAt  time.Time    `json:"createdAt"`
	Closed     bool         `json:"closed"`
	Voters     int64        `json:"voters"`
	Options    []PollOption `json:"options"`
	MyChoices  []int32      `json:"myChoices"`
}

func (s *Store) CreatePoll(ctx context.Context, tid, uid int64, v PollInput, pending bool, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := engagementConfig(ctx, tx, true)
	if err != nil {
		return err
	}
	if !c.Poll.Enabled {
		return ErrEngagementClosed
	}
	v.Question = strings.TrimSpace(v.Question)
	if !engagementText(v.Question, 200) || len(v.Options) < 2 || len(v.Options) > c.Poll.MaxOptions || v.MaxChoices < 1 || v.MaxChoices > len(v.Options) || v.DurationHours < 1 || v.DurationHours > c.Poll.MaxDays*24 {
		return ErrEngagementInvalid
	}
	seen := map[string]bool{}
	for i, text := range v.Options {
		text = strings.TrimSpace(text)
		if !engagementText(text, 200) || seen[strings.ToLower(text)] {
			return ErrEngagementInvalid
		}
		seen[strings.ToLower(text)] = true
		v.Options[i] = text
	}
	var owner int64
	var unavailable bool
	err = tx.QueryRow(ctx, `SELECT author_id,deleted OR pending OR closed FROM threads WHERE id=$1 FOR UPDATE`, tid).Scan(&owner, &unavailable)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && unavailable {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != uid {
		return ErrEngagementForbidden
	}
	if err = engagementActor(ctx, tx, uid); err != nil {
		return err
	}
	state := "published"
	if pending {
		state = "pending"
	}
	tag, err := tx.Exec(ctx, `INSERT INTO thread_polls(thread_id,question,max_choices,state,closes_at,moderation_note) VALUES($1,$2,$3,$4,clock_timestamp()+make_interval(hours=>$5),$6) ON CONFLICT DO NOTHING`, tid, v.Question, v.MaxChoices, state, v.DurationHours, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrEngagementConflict
	}
	for i, text := range v.Options {
		if _, err = tx.Exec(ctx, `INSERT INTO poll_options(thread_id,position,label) VALUES($1,$2,$3)`, tid, i+1, text); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) Poll(ctx context.Context, tid, uid int64) (Poll, error) {
	var p Poll
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT p.thread_id,p.question,p.max_choices,p.state,p.closes_at,p.created_at,
 p.state<>'published' OR p.closes_at<=statement_timestamp() OR t.closed OR t.pending OR t.deleted,p.voters,
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',o.position,'text',o.label,'votes',o.votes) ORDER BY o.position) FROM poll_options o WHERE o.thread_id=p.thread_id),'[]'),
 coalesce((SELECT choices FROM poll_ballots b WHERE b.thread_id=p.thread_id AND b.user_id=$2),'{}'::integer[])
 FROM thread_polls p JOIN threads t ON t.id=p.thread_id WHERE p.thread_id=$1`, tid, uid).Scan(&p.ThreadID, &p.Question, &p.MaxChoices, &p.State, &p.ClosesAt, &p.CreatedAt, &p.Closed, &p.Voters, &raw, &p.MyChoices)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if p.MyChoices == nil {
		p.MyChoices = []int32{}
	}
	return p, json.Unmarshal(raw, &p.Options)
}
func (s *Store) VotePoll(ctx context.Context, tid, uid int64, choices []int32) error {
	choices = append([]int32(nil), choices...)
	slices.Sort(choices)
	if len(choices) < 1 || len(choices) > 20 {
		return ErrEngagementInvalid
	}
	for i, v := range choices {
		if v < 1 || v > 20 || i > 0 && v == choices[i-1] {
			return ErrEngagementInvalid
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := engagementConfig(ctx, tx, true)
	if err != nil {
		return err
	}
	var hidden, closed bool
	err = tx.QueryRow(ctx, `SELECT deleted OR pending,closed FROM threads WHERE id=$1 FOR SHARE`, tid).Scan(&hidden, &closed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && hidden {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err = engagementActor(ctx, tx, uid); err != nil {
		return err
	}
	var state string
	var maximum int
	var until time.Time
	err = tx.QueryRow(ctx, `SELECT state,max_choices,closes_at FROM thread_polls WHERE thread_id=$1 FOR UPDATE`, tid).Scan(&state, &maximum, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var old []int32
	err = tx.QueryRow(ctx, `SELECT choices FROM poll_ballots WHERE thread_id=$1 AND user_id=$2`, tid, uid).Scan(&old)
	if err == nil {
		if slices.Equal(old, choices) {
			return nil
		}
		return ErrEngagementConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var expired bool
	if err = tx.QueryRow(ctx, `SELECT $1::timestamptz<=clock_timestamp()`, until).Scan(&expired); err != nil {
		return err
	}
	if !c.Poll.Enabled || closed || state != "published" || expired {
		return ErrEngagementClosed
	}
	if len(choices) > maximum {
		return ErrEngagementInvalid
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM poll_options WHERE thread_id=$1 AND position=ANY($2)`, tid, choices).Scan(&count); err != nil {
		return err
	}
	if count != len(choices) {
		return ErrEngagementInvalid
	}
	if _, err = tx.Exec(ctx, `INSERT INTO poll_ballots(thread_id,user_id,choices) VALUES($1,$2,$3)`, tid, uid, choices); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE poll_options SET votes=votes+1 WHERE thread_id=$1 AND position=ANY($2)`, tid, choices); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE thread_polls SET voters=voters+1 WHERE thread_id=$1`, tid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) ManagePoll(ctx context.Context, tid, actor int64, admin bool, action, reason string) error {
	if action != "close" && (!admin || action != "approve" && action != "reject") {
		return ErrEngagementInvalid
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 500 || strings.ContainsRune(reason, 0) {
		return ErrEngagementInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var owner int64
	err = tx.QueryRow(ctx, `SELECT author_id FROM threads WHERE id=$1 AND NOT deleted FOR UPDATE`, tid).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !admin && owner != actor {
		return ErrEngagementForbidden
	}
	if err = engagementActor(ctx, tx, actor); err != nil {
		return err
	}
	var state string
	if err = tx.QueryRow(ctx, `SELECT state FROM thread_polls WHERE thread_id=$1 FOR UPDATE`, tid).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	target := map[string]string{"close": "closed", "approve": "published", "reject": "rejected"}[action]
	if state == "pending" && action == "close" {
		target = "rejected"
	}
	if state == target {
		return nil
	}
	if action != "close" && state != "pending" || action == "close" && state == "rejected" {
		return ErrEngagementConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE thread_polls SET state=$2,moderation_note=$3 WHERE thread_id=$1`, tid, target, reason); err != nil {
		return err
	}
	if err = pointsAudit(ctx, tx, actor, "poll."+action, fmt.Sprintf("thread=%d reason=%s", tid, reason)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) PendingPolls(ctx context.Context, before int64) ([]Poll, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.thread_id,p.question,p.max_choices,p.state,p.closes_at,p.created_at FROM thread_polls p JOIN threads t ON t.id=p.thread_id WHERE p.state='pending' AND NOT t.deleted AND ($1::bigint=0 OR p.thread_id<$1) ORDER BY p.thread_id DESC LIMIT 50`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Poll{}
	for rows.Next() {
		var p Poll
		if err = rows.Scan(&p.ThreadID, &p.Question, &p.MaxChoices, &p.State, &p.ClosesAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Closed = true
		p.Options = []PollOption{}
		p.MyChoices = []int32{}
		out = append(out, p)
	}
	return out, rows.Err()
}
