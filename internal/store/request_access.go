package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionUser reads credentials and current account state in one snapshot.
// lastSeen controls only the optional activity write, never authorization.
func (s *Store) SessionUser(ctx context.Context, token string) (*Session, *User, time.Time, error) {
	var sess Session
	var u User
	var lastSeen time.Time
	err := s.pool.QueryRow(ctx, `SELECT s.id,s.token,s.user_id,s.csrf,s.expires_at,s.last_seen_at,
	 u.id,u.username,u.password_hash,u.email,u.group_id,u.post_count,u.signature,u.created_at,
	 u.posts_read,u.days_visited,u.email_verified,u.must_change_password,
	 coalesce(u.blocked_until,'epoch'::timestamptz)
	 FROM sessions s JOIN users u ON u.id=s.user_id
	 WHERE s.token=$1 AND s.expires_at>now() AND s.revoked_at IS NULL
	 AND (u.blocked_until IS NULL OR u.blocked_until<=now())`, hashToken(token)).Scan(
		&sess.ID, &sess.Token, &sess.UserID, &sess.CSRF, &sess.ExpiresAt, &lastSeen,
		&u.ID, &u.Username, &u.PasswordHash, &u.Email, &u.GroupID, &u.PostCount, &u.Signature, &u.CreatedAt,
		&u.PostsRead, &u.DaysVisited, &u.EmailVerified, &u.MustChangePassword, &u.BlockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return &sess, &u, lastSeen, err
}

type MembershipAccess struct {
	Config    MembershipConfig
	Member    *MemberState
	Forums    []int64
	Moderates []int64
	Banned    bool
	Quota     map[string]int64
}

// MembershipAccess always reads PostgreSQL, including explicit reauthorization.
// Quotas remain available to GET permission previews as well as write handlers.
func (s *Store) MembershipAccess(ctx context.Context, uid int64) (MembershipAccess, error) {
	var a MembershipAccess
	if uid < 0 {
		return a, ErrNotFound
	}
	var body, quota []byte
	var memberUID *int64
	var m MemberState
	err := s.pool.QueryRow(ctx, `SELECT c.version,c.body,
	 ARRAY(SELECT id::bigint FROM forums ORDER BY id),
	 ARRAY(SELECT forum_id::bigint FROM forum_moderators WHERE user_id=$1),
	 coalesce(u.banned_until>now(),false),
	 (SELECT coalesce(jsonb_object_agg(action,amount),'{}') FROM member_daily WHERE user_id=$1 AND day=`+memberDaySQL+`),
	 ms.user_id,coalesce(ms.level_id,0),coalesce(ms.experience,0),coalesce(ms.locked,false),coalesce(ms.version,0),
	 coalesce(u.days_visited,0),coalesce(u.posts_read,0),coalesce(u.post_count,0),coalesce(u.email_verified,false),
	 (coalesce(u.banned_until>now(),false) OR coalesce(u.blocked_until>now(),false))
	 FROM membership_config c LEFT JOIN users u ON u.id=$1 LEFT JOIN member_states ms ON ms.user_id=u.id WHERE c.id`, uid).Scan(
		&a.Config.Version, &body, &a.Forums, &a.Moderates, &a.Banned, &quota,
		&memberUID, &m.LevelID, &m.Experience, &m.Locked, &m.Version, &m.DaysVisited, &m.PostsRead, &m.PostCount, &m.EmailVerified, &m.Restricted)
	if err != nil {
		return a, err
	}
	version := a.Config.Version
	if err = json.Unmarshal(body, &a.Config); err != nil {
		return a, err
	}
	a.Config.Version = version
	a.Config.Normalize()
	if err = json.Unmarshal(quota, &a.Quota); err != nil {
		return a, err
	}
	if uid > 0 {
		if memberUID == nil {
			return a, ErrNotFound
		}
		m.UserID = *memberUID
		m, err = memberWithLevel(m, a.Config)
		if err != nil {
			return a, err
		}
		a.Member = &m
	}
	return a, nil
}
