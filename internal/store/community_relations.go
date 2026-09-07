// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) Follow(ctx context.Context, a, b int64) error {
	if a <= 0 || b <= 0 || a == b {
		return ErrCommunityInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockUserPair(ctx, tx, a, b); err != nil {
		return err
	}
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_pair_states p JOIN conversation_members m ON m.conversation_id=p.conversation_id
 WHERE least(p.initiator_id,p.recipient_id)=least($1::bigint,$2::bigint) AND greatest(p.initiator_id,p.recipient_id)=greatest($1::bigint,$2::bigint) AND m.blocked)`, a, b).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return ErrMessageBlocked
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND (blocked_until IS NULL OR blocked_until<=now()))`, b).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_follows(follower_id,following_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, a, b); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Unfollow(ctx context.Context, a, b int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_follows WHERE follower_id=$1 AND following_id=$2`, a, b)
	return err
}

type FollowUser struct {
	ID        int64     `json:"id,string"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"followedAt"`
}

func (s *Store) FollowUsers(ctx context.Context, uid int64, followers bool, page int) ([]FollowUser, int, error) {
	match, other := "f.follower_id", "f.following_id"
	if followers {
		match, other = other, match
	}
	filter := ` FROM user_follows f JOIN users u ON u.id=` + other + ` WHERE ` + match + `=$1 AND (u.blocked_until IS NULL OR u.blocked_until<=now())`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+filter, uid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.username,f.created_at`+filter+` ORDER BY f.created_at DESC,u.id DESC LIMIT 30 OFFSET $2`, uid, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []FollowUser{}
	for rows.Next() {
		var v FollowUser
		if err := rows.Scan(&v.ID, &v.Username, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

type Subscription struct {
	Kind        string     `json:"kind"`
	TargetID    int64      `json:"targetId,string"`
	Name        string     `json:"name,omitempty"`
	Enabled     bool       `json:"enabled"`
	NotifyInApp bool       `json:"notifyInApp"`
	NotifyEmail bool       `json:"notifyEmail"`
	MutedUntil  *time.Time `json:"mutedUntil"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func subscriptionTable(kind string) (string, string, error) {
	switch kind {
	case "thread":
		return "thread_subscriptions", "thread_id", nil
	case "forum":
		return "forum_subscriptions", "forum_id", nil
	case "tag":
		return "tag_subscriptions", "tag_id", nil
	}
	return "", "", ErrCommunityInvalid
}

// Mutations validate resources before entering this method; target SQL is whitelisted.
func (s *Store) SaveSubscription(ctx context.Context, uid int64, v Subscription, create bool) (Subscription, error) {
	table, column, err := subscriptionTable(v.Kind)
	if err != nil {
		return v, err
	}
	if v.TargetID <= 0 {
		return v, ErrCommunityInvalid
	}
	conflict := `enabled=EXCLUDED.enabled,notify_in_app=EXCLUDED.notify_in_app,notify_email=EXCLUDED.notify_email,muted_until=EXCLUDED.muted_until`
	if create {
		v.Enabled = true
		v.NotifyInApp = true
		v.NotifyEmail = true
		v.MutedUntil = nil
		conflict = `uid=EXCLUDED.uid`
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO `+table+`(uid,`+column+`,enabled,notify_in_app,notify_email,muted_until) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(uid,`+column+`) DO UPDATE SET `+conflict+` RETURNING enabled,notify_in_app,notify_email,muted_until,created_at`, uid, v.TargetID, v.Enabled, v.NotifyInApp, v.NotifyEmail, v.MutedUntil).Scan(&v.Enabled, &v.NotifyInApp, &v.NotifyEmail, &v.MutedUntil, &v.CreatedAt)
	return v, err
}
func (s *Store) DeleteSubscription(ctx context.Context, uid, target int64, kind string) error {
	table, column, err := subscriptionTable(kind)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE uid=$1 AND `+column+`=$2`, uid, target)
	return err
}
func (s *Store) Subscriptions(ctx context.Context, uid int64, kind string, page int) ([]Subscription, int, error) {
	table, col, err := subscriptionTable(kind)
	if err != nil {
		return nil, 0, err
	}
	var join string
	switch kind {
	case "thread":
		join = ` JOIN threads t ON t.id=s.thread_id AND NOT t.deleted AND NOT t.pending` + forumFilter(ctx, "t.forum_id")
	case "forum":
		join = ` JOIN forums t ON t.id=s.forum_id` + forumFilter(ctx, "t.id")
	case "tag":
		join = ` JOIN tags t ON t.id=s.tag_id`
	}
	name := "t.name"
	if kind == "thread" {
		name = "t.title"
	}
	filter := ` FROM ` + table + ` s` + join + ` WHERE s.uid=$1`
	var total int
	if err = s.pool.QueryRow(ctx, `SELECT count(*)`+filter, uid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT s.`+col+`,`+name+`,s.enabled,s.notify_in_app,s.notify_email,s.muted_until,s.created_at`+filter+` ORDER BY s.created_at DESC,s.`+col+` DESC LIMIT 30 OFFSET $2`, uid, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		v := Subscription{Kind: kind}
		if err = rows.Scan(&v.TargetID, &v.Name, &v.Enabled, &v.NotifyInApp, &v.NotifyEmail, &v.MutedUntil, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

type SubscriptionDelivery struct {
	UID   int64
	Kind  string
	InApp bool
	Email bool
}
type SubscriptionBatch struct {
	PostID     int64
	Deliveries []SubscriptionDelivery
}

// Authorization is provided by the API's existing membership resolver. A failed
// check rolls back the cursor so a transient DB error cannot silently drop recipients.
func (s *Store) ProcessSubscriptionBatch(ctx context.Context, limit int, authorize func(int64, int64) (string, bool, bool, error)) (SubscriptionBatch, error) {
	var batch SubscriptionBatch
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return batch, err
	}
	defer tx.Rollback(ctx)
	var cursor int64
	var at time.Time
	err = tx.QueryRow(ctx, `SELECT post_id,cursor_uid,created_at FROM subscription_events WHERE NOT completed ORDER BY post_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&batch.PostID, &cursor, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return batch, nil
	}
	if err != nil {
		return batch, err
	}
	var tid, fid, author int64
	var floor int
	var public bool
	err = tx.QueryRow(ctx, `SELECT t.id,t.forum_id,p.author_id,p.floor,NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted
 FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=$1`, batch.PostID).Scan(&tid, &fid, &author, &floor, &public)
	if err != nil {
		return batch, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	candidates := []SubscriptionDelivery{}
	if public {
		rows, err := tx.Query(ctx, `SELECT uid,bool_or(notify_in_app),bool_or(notify_email) FROM (
 SELECT uid,notify_in_app,notify_email FROM thread_subscriptions WHERE thread_id=$1 AND enabled AND created_at<=$4 AND (muted_until IS NULL OR muted_until<=now())
 UNION ALL SELECT uid,notify_in_app,notify_email FROM forum_subscriptions WHERE forum_id=$2 AND $3::int=1 AND enabled AND created_at<=$4 AND (muted_until IS NULL OR muted_until<=now())
 UNION ALL SELECT s.uid,s.notify_in_app,s.notify_email FROM tag_subscriptions s JOIN tags g ON g.id=s.tag_id AND g.status='active'
 JOIN thread_tags tt ON tt.tag_id=s.tag_id AND tt.thread_id=$1 WHERE $3::int=1 AND tt.created_at<=$4 AND s.enabled AND s.created_at<=$4 AND (s.muted_until IS NULL OR s.muted_until<=now())
 ) x WHERE uid>$5 AND uid<>$6 GROUP BY uid ORDER BY uid LIMIT $7`, tid, fid, floor, at, cursor, author, limit)
		if err != nil {
			return batch, err
		}
		for rows.Next() {
			var d SubscriptionDelivery
			if err = rows.Scan(&d.UID, &d.InApp, &d.Email); err != nil {
				rows.Close()
				return batch, err
			}
			candidates = append(candidates, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return batch, err
		}
	}
	for _, d := range candidates {
		cursor = d.UID
		kind, allowed, mail, err := authorize(d.UID, batch.PostID)
		if err != nil {
			return batch, err
		}
		if !allowed {
			continue
		}
		d.Kind = kind
		d.Email = d.Email && mail
		var claimed bool
		err = tx.QueryRow(ctx, `WITH ins AS (INSERT INTO subscription_deliveries(uid,post_id)
   SELECT $1::bigint,$2::bigint WHERE NOT EXISTS(SELECT 1 FROM notifications WHERE uid=$1 AND event_key='post:'||$2::bigint::text)
   ON CONFLICT DO NOTHING RETURNING uid) SELECT EXISTS(SELECT 1 FROM ins)`, d.UID, batch.PostID).Scan(&claimed)
		if err != nil {
			return batch, err
		}
		if !claimed {
			continue
		}
		if d.InApp {
			var nid int64
			err = tx.QueryRow(ctx, `SELECT forum_notify($1,p.author_id,u.username,$3,p.thread_id,p.id,left(p.content_md,60),'post:'||p.id)
   FROM posts p JOIN users u ON u.id=p.author_id WHERE p.id=$2`, d.UID, batch.PostID, d.Kind).Scan(&nid)
			if err != nil {
				return batch, err
			}
			d.InApp = nid != 0
			if nid == 0 {
				d.Email = false
			}
		}
		if d.Email {
			if err = queuePostEmail(ctx, tx, d.UID, batch.PostID, d.Kind); err != nil {
				return batch, err
			}
		}
		batch.Deliveries = append(batch.Deliveries, d)
	}
	if _, err = tx.Exec(ctx, `UPDATE subscription_events SET cursor_uid=$2,completed=$3 WHERE post_id=$1`, batch.PostID, cursor, !public || len(candidates) < limit); err != nil {
		return batch, err
	}
	return batch, tx.Commit(ctx)
}
