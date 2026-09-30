package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type SessionDevice struct {
	ID         int64      `json:"id,string"`
	Name       string     `json:"name"`
	UserAgent  string     `json:"userAgent"`
	MaskedIP   string     `json:"maskedIp"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
	Current    bool       `json:"current"`
	Status     string     `json:"status"`
}

func (s *Store) CreateDeviceSession(ctx context.Context, uid int64, ua, ip string, verifiedHash ...string) (token, csrf string, err error) {
	// Serialize creation with account blocking and bulk device revocation.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var blocked bool
	var hash string
	if err = tx.QueryRow(ctx, `SELECT coalesce(blocked_until>now(),false),password_hash FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&blocked, &hash); err != nil {
		return "", "", err
	}
	if blocked {
		return "", "", ErrNotFound
	}
	if len(verifiedHash) > 0 && verifiedHash[0] != hash {
		return "", "", ErrWrongPassword
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=$1 AND enabled)`, uid).Scan(&enabled); err != nil {
		return "", "", err
	}
	if enabled {
		return "", "", ErrMFARequired
	}
	token, csrf, err = createDeviceSessionTx(ctx, tx, uid, ua, ip)
	if err != nil {
		return "", "", err
	}
	return token, csrf, tx.Commit(ctx)
}

func createDeviceSessionTx(ctx context.Context, tx pgx.Tx, uid int64, ua, ip string) (string, string, error) {
	ua = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ua)
	if runes := []rune(ua); len(runes) > 512 {
		ua = string(runes[:512])
	}
	token, csrf := newToken(32), newToken(24)
	_, err := tx.Exec(ctx, `INSERT INTO sessions(token,user_id,csrf,expires_at,user_agent,masked_ip) VALUES($1,$2,$3,now()+$4::interval,$5,$6)`, hashToken(token), uid, csrf, sessionTTL.String(), ua, ip)
	return token, csrf, err
}

func (s *Store) TouchSession(ctx context.Context, id int64, maskedIP string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=now(),masked_ip=$2 WHERE id=$1 AND revoked_at IS NULL AND expires_at>now() AND last_seen_at<now()-interval '1 minute'`, id, maskedIP)
	return err
}

func (s *Store) DeviceSessions(ctx context.Context, uid, current, before int64) ([]SessionDevice, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,device_name,user_agent,masked_ip,created_at,last_seen_at,expires_at,revoked_at,id=$2,
 CASE WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=now() THEN 'expired' ELSE 'active' END
 FROM sessions WHERE user_id=$1 AND ($3::bigint=0 OR id<$3)
 AND expires_at>now()-interval '30 days' AND (revoked_at IS NULL OR revoked_at>now()-interval '30 days')
 ORDER BY id DESC LIMIT 51`, uid, current, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionDevice{}
	for rows.Next() {
		var v SessionDevice
		if err = rows.Scan(&v.ID, &v.Name, &v.UserAgent, &v.MaskedIP, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt, &v.RevokedAt, &v.Current, &v.Status); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var ErrDeviceName = errors.New("invalid device name")

// ManageDevice checks the calling session again inside the transaction. This prevents
// a concurrent revoked request from using its old middleware identity to change devices.
func (s *Store) ManageDevice(ctx context.Context, uid, current, target int64, action, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if action == "rename" {
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || name == "" || strings.ContainsFunc(name, unicode.IsControl) {
			return 0, ErrDeviceName
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var username string
	if err = tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1 AND NOT coalesce(blocked_until>now(),false) FOR UPDATE`, uid).Scan(&username); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>now())`, current, uid).Scan(&valid); err != nil {
		return 0, err
	}
	if !valid {
		return 0, ErrNotFound
	}
	query := ""
	switch action {
	case "rename":
		query = `UPDATE sessions SET device_name=$3 WHERE user_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()`
	case "revoke":
		query = `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()`
	case "others":
		query = `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL AND expires_at>now()`
		target = current
	case "all":
		query = `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now()`
	default:
		return 0, errors.New("invalid device action")
	}
	args := []any{uid}
	if action != "all" {
		args = append(args, target)
	}
	if action == "rename" {
		args = append(args, name)
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 && (action == "rename" || action == "revoke") {
		return 0, ErrNotFound
	}
	if action == "others" || action == "all" {
		if err = invalidateMFAChallenges(ctx, tx, uid); err != nil {
			return 0, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail) VALUES($1,$2,$3,$4)`, uid, username, "session."+action, "device="+strconv.FormatInt(target, 10))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// AdminRevokeSessions 由管理员撤销目标用户的设备会话：
// target>0 撤销单个会话，action="all" 撤销该用户全部有效会话。
// 不要求传调用者的“当前会话”，因为撤销的是他人的会话。
//
// 撤销会话与清理 MFA 挑战在同一事务内提交：任一步失败都整体回滚，
// 不会出现“接口报错但会话已被撤销”的部分成功状态。
func (s *Store) AdminRevokeSessions(ctx context.Context, uid, target int64, action string) (int64, error) {
	if uid < 1 || (action != "revoke" && action != "all") {
		return 0, errors.New("invalid device action")
	}
	if action == "revoke" && target < 1 {
		return 0, errors.New("invalid device action")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, uid).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, ErrNotFound
	}
	query := `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()`
	args := []any{uid, target}
	if action == "all" {
		query = `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now()`
		args = []any{uid}
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	// 单个会话不存在、已撤销、已过期或不属于该用户：与文档的 404 约定一致。
	if action == "revoke" && tag.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	if action == "all" {
		if _, err = tx.Exec(ctx, `DELETE FROM mfa_challenges WHERE user_id=$1`, uid); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
