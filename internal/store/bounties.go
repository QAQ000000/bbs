package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type BountyInput struct {
	Amount        int64 `json:"amount"`
	DurationHours int   `json:"durationHours"`
}
type Bounty struct {
	ThreadID      int64      `json:"threadId,string"`
	OwnerID       int64      `json:"ownerId,string"`
	Amount        int64      `json:"amount"`
	DurationHours int        `json:"durationHours"`
	State         string     `json:"state"`
	ClosesAt      time.Time  `json:"closesAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	SettledAt     *time.Time `json:"settledAt"`
	RecipientID   int64      `json:"recipientId,string"`
	PostID        int64      `json:"postId,string"`
	RuleVersion   int64      `json:"ruleVersion"`
	Note          string     `json:"note"`
}

const bountySelect = `SELECT thread_id,owner_id,amount,duration_hours,state,closes_at,created_at,settled_at,coalesce(recipient_id,0),coalesce(post_id,0),rule_version,note FROM thread_bounties`

func bountyFields(b *Bounty) []any {
	return []any{&b.ThreadID, &b.OwnerID, &b.Amount, &b.DurationHours, &b.State, &b.ClosesAt, &b.CreatedAt, &b.SettledAt, &b.RecipientID, &b.PostID, &b.RuleVersion, &b.Note}
}
func scanBounty(row pgx.Row) (Bounty, error) {
	var b Bounty
	err := row.Scan(bountyFields(&b)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}
func (s *Store) Bounty(ctx context.Context, tid int64) (Bounty, error) {
	return scanBounty(s.pool.QueryRow(ctx, bountySelect+` WHERE thread_id=$1`, tid))
}
func (s *Store) CreateBounty(ctx context.Context, tid, uid int64, v BountyInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := engagementConfig(ctx, tx, true)
	if err != nil {
		return err
	}
	var owner int64
	var hidden bool
	err = tx.QueryRow(ctx, `SELECT author_id,deleted OR pending OR closed FROM threads WHERE id=$1 FOR UPDATE`, tid).Scan(&owner, &hidden)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && hidden {
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
	old, err := scanBounty(tx.QueryRow(ctx, bountySelect+` WHERE thread_id=$1 FOR UPDATE`, tid))
	if err == nil {
		if old.Amount == v.Amount && old.DurationHours == v.DurationHours {
			return nil
		}
		return ErrEngagementConflict
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if !c.Bounty.Enabled {
		return ErrEngagementClosed
	}
	if v.Amount < c.Bounty.MinPoints || v.Amount > c.Bounty.MaxPoints || v.DurationHours < 1 || v.DurationHours > c.Bounty.MaxDays*24 {
		return ErrEngagementInvalid
	}
	var accepted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accepted_replies WHERE thread_id=$1)`, tid).Scan(&accepted); err != nil {
		return err
	}
	if accepted {
		return ErrEngagementConflict
	}
	a, err := lockPoints(ctx, tx, uid)
	if err != nil {
		return err
	}
	if a.Balance-a.Frozen < v.Amount {
		return ErrPointsInsufficient
	}
	var created time.Time
	err = tx.QueryRow(ctx, `INSERT INTO thread_bounties(thread_id,owner_id,amount,duration_hours,closes_at,rule_version) VALUES($1,$2,$3,$4,clock_timestamp()+make_interval(hours=>$4),$5) RETURNING created_at`, tid, uid, v.Amount, v.DurationHours, c.Version).Scan(&created)
	if err != nil {
		return err
	}
	if err = appendPoints(ctx, tx, a, PointsEntry{Source: fmt.Sprintf("bounty:%d:freeze", tid), Kind: "bounty", FrozenDelta: v.Amount, RuleVersion: c.Version, Reason: "创建主题悬赏，冻结积分", ActorID: uid, EventAt: created}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Caller holds thread and selected post locks. Both accounts are locked in ID
// order; the reward and accepted_replies insertion share this transaction.
func awardBounty(ctx context.Context, tx pgx.Tx, tid, pid, owner, recipient int64) error {
	b, err := scanBounty(tx.QueryRow(ctx, bountySelect+` WHERE thread_id=$1 FOR UPDATE`, tid))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.State == "awarded" {
		if b.PostID == pid {
			return nil
		}
		return ErrEngagementConflict
	}
	if b.State != "active" {
		return nil
	}
	var expired bool
	if err = tx.QueryRow(ctx, `SELECT $1::timestamptz<=clock_timestamp()`, b.ClosesAt).Scan(&expired); err != nil {
		return err
	}
	if expired {
		return ErrEngagementClosed
	}
	if owner != b.OwnerID || owner == recipient {
		return ErrEngagementForbidden
	}
	first, second := owner, recipient
	if first > second {
		first, second = second, first
	}
	a1, err := lockPoints(ctx, tx, first)
	if err != nil {
		return err
	}
	a2, err := lockPoints(ctx, tx, second)
	if err != nil {
		return err
	}
	payer, payee := a1, a2
	if payer.UserID != owner {
		payer, payee = payee, payer
	}
	if payer.Frozen < b.Amount {
		return ErrEngagementConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if err = appendPoints(ctx, tx, payer, PointsEntry{Source: fmt.Sprintf("bounty:%d:award", tid), Kind: "bounty", Delta: -b.Amount, FrozenDelta: -b.Amount, RuleVersion: b.RuleVersion, Reason: "采纳回复，支付悬赏", ActorID: owner, EventAt: now}); err != nil {
		return err
	}
	if err = appendPoints(ctx, tx, payee, PointsEntry{Source: fmt.Sprintf("bounty:%d:receive", tid), Kind: "bounty", Delta: b.Amount, RuleVersion: b.RuleVersion, Reason: "回复被采纳，获得悬赏", ActorID: owner, EventAt: now}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE thread_bounties SET state='awarded',recipient_id=$2,post_id=$3,settled_at=$4,refund_error_code='',refund_failed_at=NULL,refund_next_attempt_at=NULL WHERE thread_id=$1`, tid, recipient, pid, now)
	return err
}
func preventBountyUnaccept(ctx context.Context, tx pgx.Tx, tid int64) error {
	var awarded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM thread_bounties WHERE thread_id=$1 AND state='awarded')`, tid).Scan(&awarded); err != nil {
		return err
	}
	if awarded {
		return ErrEngagementConflict
	}
	return nil
}

// Background callers use SKIP LOCKED and the same thread->bounty->account order.
func (s *Store) RefundBounty(ctx context.Context, tid, actor int64, admin, automatic bool, reason string) (bool, error) {
	reason = strings.TrimSpace(reason)
	if !automatic && admin && !engagementText(reason, 500) {
		return false, ErrEngagementInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if automatic {
		if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
			return false, err
		}
	}

	threadSQL := `SELECT deleted FROM threads WHERE id=$1 FOR UPDATE`
	if automatic {
		threadSQL += " SKIP LOCKED"
	}
	var deleted bool
	err = tx.QueryRow(ctx, threadSQL, tid).Scan(&deleted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM threads WHERE id=$1)`, tid).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
		deleted = true
	}
	sql := bountySelect + ` WHERE thread_id=$1`
	if automatic {
		sql += ` AND (refund_next_attempt_at IS NULL OR refund_next_attempt_at<=clock_timestamp())`
	}
	sql += ` FOR UPDATE`
	if automatic {
		sql += " SKIP LOCKED"
	}
	b, err := scanBounty(tx.QueryRow(ctx, sql, tid))
	if errors.Is(err, ErrNotFound) && automatic {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !automatic {
		if err = engagementActor(ctx, tx, actor); err != nil {
			return false, err
		}
		if !admin && actor != b.OwnerID {
			return false, ErrEngagementForbidden
		}
	}
	if b.State != "active" {
		if b.State == "awarded" && !automatic {
			return false, ErrEngagementConflict
		}
		return false, nil
	}
	var expired bool
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT $1::timestamptz<=clock_timestamp(),clock_timestamp()`, b.ClosesAt).Scan(&expired, &now); err != nil {
		return false, err
	}
	state := "canceled"
	if automatic {
		if !expired && !deleted {
			return false, nil
		}
		reason = "主题删除，退回悬赏"
		if expired {
			state = "expired"
			reason = "悬赏到期，退回冻结积分"
		}
	} else if !admin && !expired && !deleted {
		var replies bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM posts WHERE thread_id=$1 AND floor>1 AND NOT deleted AND NOT pending)`, tid).Scan(&replies); err != nil {
			return false, err
		}
		if replies {
			return false, ErrEngagementConflict
		}
		reason = "作者取消悬赏"
	}
	if expired {
		state = "expired"
	}
	if reason == "" {
		reason = "作者取消或领取到期退款"
	}
	a, err := lockPoints(ctx, tx, b.OwnerID)
	if err != nil {
		return false, err
	}
	if a.Frozen < b.Amount {
		return false, ErrEngagementConflict
	}
	if err = appendPoints(ctx, tx, a, PointsEntry{Source: fmt.Sprintf("bounty:%d:refund", tid), Kind: "bounty", FrozenDelta: -b.Amount, RuleVersion: b.RuleVersion, Reason: reason, ActorID: actor, EventAt: now}); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE thread_bounties SET state=$2,settled_at=$3,note=$4,refund_error_code='',refund_failed_at=NULL,refund_next_attempt_at=NULL WHERE thread_id=$1`, tid, state, now, reason); err != nil {
		return false, err
	}
	if admin && !automatic {
		if err = pointsAudit(ctx, tx, actor, "bounty.cancel", fmt.Sprintf("thread=%d reason=%s", tid, reason)); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
func (s *Store) ProcessBountyRefunds(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.thread_id FROM thread_bounties b LEFT JOIN threads t ON t.id=b.thread_id WHERE b.state='active' AND (b.refund_next_attempt_at IS NULL OR b.refund_next_attempt_at<=now()) AND (b.closes_at<=now() OR t.id IS NULL OR t.deleted) ORDER BY b.closes_at,b.thread_id LIMIT 100`)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	n := 0
	for _, tid := range ids {
		changed, err := s.RefundBounty(ctx, tid, 0, false, true, "")
		if err != nil {
			code := bountyRefundErrorCode(err)
			if code == "" || ctx.Err() != nil {
				return n, err
			}
			// Preserve only stable diagnostic codes, never raw SQL or user data.
			_, recordErr := s.pool.Exec(ctx, `UPDATE thread_bounties SET refund_attempts=LEAST(refund_attempts+1,1000000),refund_error_code=$2,refund_failed_at=clock_timestamp(),refund_next_attempt_at=clock_timestamp()+make_interval(secs=>LEAST(3600,30*power(2,LEAST(refund_attempts,7)))::double precision) WHERE thread_id=$1 AND state='active' AND (refund_next_attempt_at IS NULL OR refund_next_attempt_at<=clock_timestamp())`, tid, code)
			if recordErr != nil {
				return n, recordErr
			}
			continue
		}
		if changed {
			n++
		}
	}
	return n, nil
}

// Connectivity, cancellation, capacity and unknown errors stop the batch. Known
// transaction/business failures are isolated and retried with bounded backoff.
func bountyRefundErrorCode(err error) string {
	if errors.Is(err, ErrEngagementConflict) {
		return "FROZEN_BALANCE_MISMATCH"
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && len(pgerr.Code) >= 2 {
		switch pgerr.Code[:2] {
		case "23", "P0":
			return "REFUND_TRANSACTION_FAILED"
		case "40", "55":
			return "REFUND_RETRYABLE_CONFLICT"
		}
	}
	return ""
}

type AdminBounty struct {
	Bounty
	RefundAttempts      int        `json:"refundAttempts"`
	RefundErrorCode     string     `json:"refundErrorCode"`
	RefundFailedAt      *time.Time `json:"refundFailedAt"`
	RefundNextAttemptAt *time.Time `json:"refundNextAttemptAt"`
}

var adminBountySelect = strings.Replace(bountySelect, " FROM thread_bounties", ",refund_attempts,refund_error_code,refund_failed_at,refund_next_attempt_at FROM thread_bounties", 1)

func scanAdminBounty(row pgx.Row) (AdminBounty, error) {
	var b AdminBounty
	err := row.Scan(append(bountyFields(&b.Bounty), &b.RefundAttempts, &b.RefundErrorCode, &b.RefundFailedAt, &b.RefundNextAttemptAt)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}
func (s *Store) AdminBounty(ctx context.Context, tid int64) (AdminBounty, error) {
	return scanAdminBounty(s.pool.QueryRow(ctx, adminBountySelect+` WHERE thread_id=$1`, tid))
}
func (s *Store) AdminBounties(ctx context.Context, before int64, state string, failed bool) ([]AdminBounty, error) {
	switch state {
	case "active", "awarded", "canceled", "expired", "all":
	default:
		return nil, ErrEngagementInvalid
	}
	rows, err := s.pool.Query(ctx, adminBountySelect+` WHERE ($1::bigint=0 OR thread_id<$1) AND ($2='all' OR state=$2) AND (NOT $3::boolean OR refund_error_code<>'') ORDER BY thread_id DESC LIMIT 50`, before, state, failed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminBounty{}
	for rows.Next() {
		b, err := scanAdminBounty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type BountyRefundDiagnostics struct {
	Active      int64      `json:"active"`
	Due         int64      `json:"due"`
	Failed      int64      `json:"failed"`
	Scheduled   int64      `json:"scheduled"`
	OldestDueAt *time.Time `json:"oldestDueAt"`
}

func (s *Store) BountyRefundDiagnostics(ctx context.Context) (BountyRefundDiagnostics, error) {
	var d BountyRefundDiagnostics
	err := s.pool.QueryRow(ctx, `WITH q AS (SELECT b.*,b.closes_at<=now() OR t.id IS NULL OR t.deleted AS due FROM thread_bounties b LEFT JOIN threads t ON t.id=b.thread_id WHERE b.state='active') SELECT count(*),count(*) FILTER(WHERE due),count(*) FILTER(WHERE refund_error_code<>''),count(*) FILTER(WHERE due AND refund_next_attempt_at>now()),min(closes_at) FILTER(WHERE due) FROM q`).Scan(&d.Active, &d.Due, &d.Failed, &d.Scheduled, &d.OldestDueAt)
	return d, err
}

// Queue a failed refund for the ordinary worker. It never bypasses settlement
// conditions or changes money; the audit and scheduling update are atomic.
func (s *Store) RetryBountyRefund(ctx context.Context, tid, actor int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if !engagementText(reason, 500) {
		return ErrEngagementInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = engagementActor(ctx, tx, actor); err != nil {
		return err
	}
	b, err := scanAdminBounty(tx.QueryRow(ctx, adminBountySelect+` WHERE thread_id=$1 FOR UPDATE`, tid))
	if err != nil {
		return err
	}
	if b.State != "active" || b.RefundErrorCode == "" {
		return ErrEngagementConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if b.RefundNextAttemptAt != nil && !b.RefundNextAttemptAt.After(now) {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE thread_bounties SET refund_next_attempt_at=clock_timestamp() WHERE thread_id=$1`, tid); err != nil {
		return err
	}
	if err = pointsAudit(ctx, tx, actor, "bounty.refund.retry", fmt.Sprintf("thread=%d reason=%s", tid, reason)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
