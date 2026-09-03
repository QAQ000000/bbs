// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"time"
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
