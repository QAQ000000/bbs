// SPDX-License-Identifier: AGPL-3.0-or-later
// store/notify.go：上传记录与站内通知（@提及）。
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// ---- 上传记录 ----

// Upload 一条上传记录。
type Upload struct {
	ID        int64
	UID       int64
	Name      string
	Path      string
	Size      int64
	Mime      string
	CreatedAt time.Time
}

// SaveUpload 记录一次上传。
func (s *Store) SaveUpload(ctx context.Context, uid int64, name, path string, size int64, mime string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO uploads (uid, name, path, size, mime) VALUES ($1,$2,$3,$4,$5)`,
		uid, name, path, size, mime)
	return err
}

// ---- 通知 ----

// Notification 一条通知。
type Notification struct {
	ID        int64
	UID       int64
	FromUID   int64
	FromName  string
	Type      string
	ThreadID  int64
	PostID    int64
	Excerpt   string
	Read      bool
	CreatedAt time.Time
}

// AddNotifications 批量插入通知（去重与排除自己由调用方处理）。
func (s *Store) AddNotifications(ctx context.Context, rows []*Notification) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, n := range rows {
		if _, err := tx.Exec(ctx,
			`INSERT INTO notifications (uid, from_uid, from_name, type, thread_id, post_id, excerpt)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			n.UID, n.FromUID, n.FromName, n.Type, n.ThreadID, n.PostID, n.Excerpt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UnreadCount 未读通知数。
func (s *Store) UnreadCount(ctx context.Context, uid int64) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE uid=$1 AND NOT read`, uid).Scan(&n)
	return n
}

// Notifications 通知列表（最新在前）。
func (s *Store) Notifications(ctx context.Context, uid int64, limit int) ([]*Notification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, uid, from_uid, from_name, type, thread_id, post_id, excerpt, read, created_at
		 FROM notifications WHERE uid=$1 ORDER BY id DESC LIMIT $2`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UID, &n.FromUID, &n.FromName, &n.Type,
			&n.ThreadID, &n.PostID, &n.Excerpt, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// MarkNotificationsRead 标记全部已读。
func (s *Store) MarkNotificationsRead(ctx context.Context, uid int64) {
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET read=true WHERE uid=$1 AND NOT read`, uid)
	_ = err
}

// ---- 密码重置 ----

// CreatePasswordReset 为用户创建重置令牌（库中只存哈希），有效期 15 分钟。
func (s *Store) CreatePasswordReset(ctx context.Context, uid int64) (string, error) {
	raw := newToken(32)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO password_resets (uid, token_hash, expires_at)
		 VALUES ($1,$2, now() + interval '15 minutes')`, uid, hashToken(raw))
	return raw, err
}

// ResetUIDByToken 校验重置令牌（未用、未过期），返回用户 id。
func (s *Store) ResetUIDByToken(ctx context.Context, raw string) (int64, error) {
	var uid int64
	err := s.pool.QueryRow(ctx,
		`SELECT uid FROM password_resets
		 WHERE token_hash=$1 AND NOT used AND expires_at > now()`,
		hashToken(raw)).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

// ConsumePasswordReset 作废令牌并强制该用户全部会话下线。
func (s *Store) ConsumePasswordReset(ctx context.Context, raw string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE password_resets SET used=true WHERE token_hash=$1`, hashToken(raw)); err != nil {
		return err
	}
	var uid int64
	if err := s.pool.QueryRow(ctx,
		`SELECT uid FROM password_resets WHERE token_hash=$1`, hashToken(raw)).Scan(&uid); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, uid)
	s.sessions.invalidateUser(uid)
	return err
}

// UpdateProfile 更新签名与邮箱（邮箱唯一性由 users_email_unique_idx 兜底）。
func (s *Store) UpdateProfile(ctx context.Context, uid int64, signature, email string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET signature=$2, email=$3 WHERE id=$1`, uid, signature, email)
	return err
}

// ChangePassword 修改密码：校验旧密码；成功后撤销当前会话之外的全部会话。
// keepRawToken 为当前设备的原始会话 token（空则撤销全部会话）。
func (s *Store) ChangePassword(ctx context.Context, uid int64, oldPassword, newPassword, keepRawToken string) error {
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, uid).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return ErrWrongPassword
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, uid, string(newHash)); err != nil {
		return err
	}
	if keepRawToken != "" {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM sessions WHERE user_id=$1 AND token<>$2`, uid, hashToken(keepRawToken)); err != nil {
			return err
		}
	} else if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, uid); err != nil {
		return err
	}
	s.sessions.invalidateUser(uid)
	return nil
}

// UpdatePassword 重设密码（bcrypt）。
func (s *Store) UpdatePassword(ctx context.Context, uid int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, uid, string(hash))
	return err
}

// UserEmailExists 是否存在使用该邮箱的账号（找回流程的前置查询）。
func (s *Store) UIDByEmail(ctx context.Context, email string) (int64, error) {
	var uid int64
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM users WHERE lower(email)=lower($1) AND email <> ''`, email).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}
