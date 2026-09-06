// SPDX-License-Identifier: AGPL-3.0-or-later
// store/notify.go：上传记录与站内通知（@提及）。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	PostID    int64
	CreatedAt time.Time
}

// SaveUpload 记录一次上传。
func (s *Store) SaveUpload(ctx context.Context, uid int64, name, path string, size int64, mime string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO uploads (uid, name, path, size, mime) VALUES ($1,$2,$3,$4,$5)`,
		uid, name, path, size, mime)
	return err
}

// uploadsPathRe 无需编译期注册：见 LinkUploadsToPost（放在 web 层提取路径，本层收数组）。

// LinkUploadsToPost 把内容中引用的上传挂到楼层：先挂新引用（含本楼层改挂），
// 再把本楼层已不再引用的旧附件解挂。
func (s *Store) LinkUploadsToPost(ctx context.Context, uid, postID int64, paths []string) error {
	if len(paths) == 0 {
		_, err := s.pool.Exec(ctx,
			`UPDATE uploads SET post_id=NULL WHERE post_id=$2 AND uid=$1`, uid, postID)
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE uploads SET post_id=$3 WHERE uid=$1 AND path = ANY($2) AND (post_id IS NULL OR post_id=$3)`,
		uid, paths, postID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE uploads SET post_id=NULL WHERE uid=$1 AND post_id=$3 AND NOT (path = ANY($2))`,
		uid, paths, postID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UploadVisibility 上传文件的可见性要素（挂载楼层与所属主题的状态）。
type UploadVisibility struct {
	UID           int64
	PostID        int64
	PostPending   bool
	PostDeleted   bool
	ThreadPending bool
	ThreadDeleted bool
}

// UploadByPath 按站点路径（/uploads/...）查上传记录及挂载楼层/主题状态。
// 无记录返回 ErrNotFound：未登记的磁盘文件一律不可经 HTTP 访问。
func (s *Store) UploadByPath(ctx context.Context, path string) (*UploadVisibility, error) {
	var v UploadVisibility
	err := s.pool.QueryRow(ctx, `
		SELECT u.uid, coalesce(u.post_id,0),
		       coalesce(p.pending,false), coalesce(p.deleted,false),
		       coalesce(t.pending,false), coalesce(t.deleted,false)
		FROM uploads u
		LEFT JOIN posts p ON p.id = u.post_id
		LEFT JOIN threads t ON t.id = p.thread_id
		WHERE u.path=$1`, path).
		Scan(&v.UID, &v.PostID, &v.PostPending, &v.PostDeleted, &v.ThreadPending, &v.ThreadDeleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &v, err
}

// UploadsForPosts 楼层附件列表（按楼层分组返回）。
func (s *Store) UploadsForPosts(ctx context.Context, postIDs []int64) (map[int64][]Upload, error) {
	out := map[int64][]Upload{}
	if len(postIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, uid, name, path, size, mime, post_id FROM uploads
		 WHERE post_id = ANY($1) ORDER BY id`, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.ID, &u.UID, &u.Name, &u.Path, &u.Size, &u.Mime, &u.PostID); err != nil {
			return nil, err
		}
		out[u.PostID] = append(out[u.PostID], u)
	}
	return out, rows.Err()
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
	Scope     string
	Payload   json.RawMessage
	EventKey  string
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
		if err := tx.QueryRow(ctx,
			`SELECT forum_notify($1,$2,$3,$4,$5,$6,$7,nullif($8,''))`,
			n.UID, n.FromUID, n.FromName, n.Type, n.ThreadID, n.PostID, n.Excerpt, n.EventKey).Scan(&n.ID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UnreadCount 未读通知数。
func (s *Store) UnreadCount(ctx context.Context, uid int64) int64 {
	n, _ := s.NotificationUnreadCount(ctx, uid)
	return n
}

func notificationVisibility(ctx context.Context) string {
	return ` AND (scope='account' OR EXISTS(SELECT 1 FROM threads t JOIN posts p ON p.thread_id=t.id
 WHERE t.id=notifications.thread_id AND p.id=notifications.post_id AND NOT t.deleted AND NOT t.pending
 AND NOT p.deleted AND NOT p.pending` + forumFilter(ctx, "t.forum_id") + `))`
}

func (s *Store) NotificationUnreadCount(ctx context.Context, uid int64) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND NOT read`+notificationVisibility(ctx), uid).Scan(&n)
	return n, err
}

// Notifications 通知列表（最新在前）。
func (s *Store) Notifications(ctx context.Context, uid int64, limit int) ([]*Notification, error) {
	rows, _, err := s.NotificationPage(ctx, uid, 1, limit, false)
	return rows, err
}

func (s *Store) NotificationPage(ctx context.Context, uid int64, page, size int, unread bool) ([]*Notification, int, error) {
	filter := ` WHERE uid=$1 AND (NOT $2::bool OR NOT read)` + notificationVisibility(ctx)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications`+filter, uid, unread).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, uid, from_uid, from_name, type, thread_id, post_id, excerpt, read, created_at,scope,payload
		 FROM notifications`+filter+` ORDER BY id DESC LIMIT $3 OFFSET $4`, uid, unread, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UID, &n.FromUID, &n.FromName, &n.Type,
			&n.ThreadID, &n.PostID, &n.Excerpt, &n.Read, &n.CreatedAt, &n.Scope, &n.Payload); err != nil {
			return nil, 0, err
		}
		out = append(out, &n)
	}
	return out, total, rows.Err()
}

// MarkNotificationsRead 标记全部已读。
func (s *Store) MarkNotificationsRead(ctx context.Context, uid int64) error {
	_, err := s.ReadNotifications(ctx, uid, nil, true)
	return err
}

func (s *Store) ReadNotifications(ctx context.Context, uid int64, ids []int64, all bool) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE notifications SET read=true WHERE uid=$1 AND NOT read
 AND ($2::bool OR id=ANY($3::bigint[]))`+notificationVisibility(ctx), uid, all, ids)
	return tag.RowsAffected(), err
}

var NotificationPreferenceKeys = []string{"mentions", "replies", "acceptance", "membership", "titles", "moderation", "reports", "email"}

func (s *Store) NotificationPreferences(ctx context.Context, uid int64) (map[string]bool, error) {
	out := map[string]bool{}
	for _, key := range NotificationPreferenceKeys {
		out[key] = true
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT body FROM notification_preferences WHERE uid=$1`, uid).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func (s *Store) SaveNotificationPreferences(ctx context.Context, uid int64, prefs map[string]bool) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO notification_preferences(uid,body) VALUES($1,$2)
 ON CONFLICT(uid) DO UPDATE SET body=EXCLUDED.body`, uid, raw)
	return err
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

// ErrTokenInvalid 令牌无效、已使用或已过期。
var ErrTokenInvalid = errors.New("token invalid")

// ResetPasswordByToken 原子完成密码重置：单事务内条件消费令牌（未用且未过期，
// 只允许一个并发请求成功）→ 撤销该用户其余全部重置令牌（旧链接随即失效）→
// 写入新密码 → 撤销全部会话。此前「校验→改密→消费」三步非原子，
// 同一令牌并发提交可两次改密。
func (s *Store) ResetPasswordByToken(ctx context.Context, raw, newPassword string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var uid int64
	err = tx.QueryRow(ctx,
		`UPDATE password_resets SET used=true
		 WHERE token_hash=$1 AND NOT used AND expires_at > now()
		 RETURNING uid`, hashToken(raw)).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrTokenInvalid
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE password_resets SET used=true WHERE uid=$1 AND NOT used`, uid); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET password_hash=$2, must_change_password=false WHERE id=$1`, uid, string(hash)); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, uid); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	s.sessions.invalidateUser(uid)
	return uid, nil
}

// UpdateProfile 更新签名与邮箱（邮箱唯一性由 users_email_unique_idx 兜底）。
// 邮箱变更即清除 email_verified 并作废旧验证令牌：验证状态跟随具体邮箱，
// 换绑后不能沿用旧邮箱的验证结论。返回邮箱是否发生变更。
func (s *Store) UpdateProfile(ctx context.Context, uid int64, signature, email string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var oldEmail string
	if err := tx.QueryRow(ctx,
		`SELECT email FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&oldEmail); err != nil {
		return false, err
	}
	changed := oldEmail != email
	if _, err := tx.Exec(ctx, `
		UPDATE users SET signature=$2, email=$3,
		       email_verified = CASE WHEN email=$3 THEN email_verified ELSE false END
		WHERE id=$1`, uid, signature, email); err != nil {
		return false, err
	}
	if changed {
		if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE uid=$1`, uid); err != nil {
			return false, err
		}
	}
	return changed, tx.Commit(ctx)
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
	if _, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$2, must_change_password=false WHERE id=$1`, uid, string(newHash)); err != nil {
		return err
	}
	// 改密后撤销全部未用重置令牌：持旧链接者不能在用户改密后重新控制账号
	if _, err := s.pool.Exec(ctx,
		`UPDATE password_resets SET used=true WHERE uid=$1 AND NOT used`, uid); err != nil {
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

// CreateEmailVerify 生成邮箱验证令牌（24h 有效；库中存哈希，同用户覆盖旧令牌）。
// 令牌绑定申请时的邮箱地址：换绑后旧链接不能验证新邮箱。
func (s *Store) CreateEmailVerify(ctx context.Context, uid int64, email string) (string, error) {
	raw := newToken(24)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO email_verifications (uid, token_hash, expires_at, email)
		VALUES ($1,$2, now() + interval '24 hours', $3)
		ON CONFLICT (uid) DO UPDATE SET
			token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at,
			created_at = now(), email = EXCLUDED.email`,
		uid, hashToken(raw), strings.ToLower(email))
	return raw, err
}

// ConsumeEmailVerify 一次性消费验证令牌；仅当用户当前邮箱与令牌绑定的
// 邮箱一致时标记已验证。返回 (uid, 是否已验证)。
func (s *Store) ConsumeEmailVerify(ctx context.Context, raw string) (int64, bool, error) {
	var uid int64
	var email string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM email_verifications WHERE token_hash=$1 AND expires_at > now() RETURNING uid, email`,
		hashToken(raw)).Scan(&uid, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET email_verified=true WHERE id=$1 AND lower(email)=$2`, uid, email)
	if err != nil {
		return uid, false, err
	}
	return uid, tag.RowsAffected() > 0, nil
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
