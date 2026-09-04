// SPDX-License-Identifier: AGPL-3.0-or-later
// store/admin.go：后台管理数据访问（版块/用户/内容批量操作/审计日志/仪表盘统计）。
package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- 管理后台数据访问 ----

// AdminLog 写一条后台操作日志。
func (s *Store) AdminLog(ctx context.Context, uid int64, username, action, detail, ip string) {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO admin_logs (uid, username, action, detail, ip) VALUES ($1,$2,$3,$4,$5)`,
		uid, username, action, detail, ip)
	_ = err
}

// AdminLogs 日志分页。
func (s *Store) AdminLogs(ctx context.Context, page, size int) ([]*AdminLogEntry, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, uid, username, action, detail, ip, created_at
		 FROM admin_logs ORDER BY id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AdminLogEntry
	for rows.Next() {
		var l AdminLogEntry
		if err := rows.Scan(&l.ID, &l.UID, &l.Username, &l.Action, &l.Detail, &l.IP, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &l)
	}
	return out, total, rows.Err()
}

// AdminLogEntry 后台操作日志行。
type AdminLogEntry struct {
	ID        int64
	UID       int64
	Username  string
	Action    string
	Detail    string
	IP        string
	CreatedAt time.Time
}

// ---- 版块管理 ----

// SaveForum 新建或更新版块；id=0 为新建。moderators 为用户名 CSV，写入
// forums.moderators 展示字段的同时同步 forum_moderators 关系表（权威数据）。
func (s *Store) SaveForum(ctx context.Context, id int64, categoryID int, name, description, moderators string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("版块名称不能为空")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if id == 0 {
		var maxOrder int
		if err := tx.QueryRow(ctx,
			`SELECT coalesce(max(displayorder),0)+1 FROM forums WHERE category_id=$1`, categoryID).Scan(&maxOrder); err != nil {
			return 0, err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO forums (category_id, name, description, displayorder, moderators) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			categoryID, name, description, maxOrder, moderators).Scan(&id); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.Exec(ctx,
			`UPDATE forums SET category_id=$2, name=$3, description=$4, moderators=$5 WHERE id=$1`,
			id, categoryID, name, description, moderators); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_moderators WHERE forum_id=$1`, id); err != nil {
		return 0, err
	}
	names := splitModeratorNames(moderators)
	if len(names) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO forum_moderators (forum_id, user_id)
			SELECT $1, u.id FROM users u WHERE lower(u.username) = ANY($2)
			ON CONFLICT DO NOTHING`, id, names); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit(ctx)
}

func splitModeratorNames(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\t'
	}) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

// DeleteForum 删除空版块（有主题时拒绝）。
func (s *Store) DeleteForum(ctx context.Context, id int64) error {
	var n int64
	if err := s.pool.QueryRow(ctx,
		`SELECT thread_count FROM forums WHERE id=$1`, id).Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if n > 0 {
		return errors.New("版块下仍有主题，请先清空或转移")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM forums WHERE id=$1`, id)
	return err
}

// SaveCategory 新建或更新分类。
func (s *Store) SaveCategory(ctx context.Context, id int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("分类名称不能为空")
	}
	if id == 0 {
		var maxOrder int
		if err := s.pool.QueryRow(ctx, `SELECT coalesce(max(displayorder),0)+1 FROM categories`).Scan(&maxOrder); err != nil {
			return err
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO categories (name, displayorder) VALUES ($1,$2)`, name, maxOrder)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE categories SET name=$2 WHERE id=$1`, id, name)
	return err
}

// DeleteCategory 删除空分类。
func (s *Store) DeleteCategory(ctx context.Context, id int) error {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM forums WHERE category_id=$1`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("分类下仍有版块")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM categories WHERE id=$1`, id)
	return err
}

// MoveForum 同级内上移/下移（交换相邻 displayorder）。
func (s *Store) MoveForum(ctx context.Context, id int64, up bool) error {
	var cur struct {
		cat   int
		order int
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT category_id, displayorder FROM forums WHERE id=$1`, id).Scan(&cur.cat, &cur.order); err != nil {
		return err
	}
	q := `SELECT id, displayorder FROM forums WHERE category_id=$1 AND id<>$2 AND displayorder `
	q += map[bool]string{true: "< $3 ORDER BY displayorder DESC LIMIT 1", false: "> $3 ORDER BY displayorder ASC LIMIT 1"}[up]
	var oid int64
	var oorder int
	if err := s.pool.QueryRow(ctx, q, cur.cat, id, cur.order).Scan(&oid, &oorder); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // 已在边界，视为成功
		}
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE forums SET displayorder=$2 WHERE id=$1`, id, oorder); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE forums SET displayorder=$2 WHERE id=$1`, oid, cur.order); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- 内容管理 ----

// ThreadQuery 主题搜索条件。
type ThreadQuery struct {
	ForumID        int64  // 0 = 全部
	Keyword        string // 标题模糊
	Author         string // 用户名精确
	Page           int
	Size           int
	OnlyForumIDs   []int64 // 非空时限定版块范围（版主管辖）
	PendingOnly    bool    // 仅待审核
	IncludePending bool    // 包含待审核（内容管理列表）
}

func (q *ThreadQuery) where(args []any) (string, []any) {
	w := ` WHERE NOT t.deleted`
	if q.PendingOnly {
		w += ` AND t.pending`
	} else if !q.IncludePending {
		w += ` AND NOT t.pending`
	}
	if q.ForumID > 0 {
		args = append(args, q.ForumID)
		w += ` AND t.forum_id = $` + strconv.Itoa(len(args))
	}
	if q.Keyword != "" {
		args = append(args, "%"+q.Keyword+"%")
		w += ` AND t.title ILIKE $` + strconv.Itoa(len(args))
	}
	if q.Author != "" {
		args = append(args, q.Author)
		w += ` AND u.username = $` + strconv.Itoa(len(args))
	}
	if len(q.OnlyForumIDs) > 0 {
		args = append(args, q.OnlyForumIDs)
		w += ` AND t.forum_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	return w, args
}

// SearchThreads 后台主题搜索。
func (s *Store) SearchThreads(ctx context.Context, q ThreadQuery) ([]*Thread, int, error) {
	var args []any
	w, args := q.where(args)

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM threads t JOIN users u ON u.id=t.author_id`+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, q.Size, (q.Page-1)*q.Size)
	rows, err := s.pool.Query(ctx,
		`SELECT `+threadCols+` `+threadJoins+w+` ORDER BY t.last_post_at DESC LIMIT $`+
			strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2), qArgs...)
	list, err := collectThreads(rows, err)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// AdminThreadAction 批量执行主题管理动作，返回受影响的主题（用于广播）。
// op: sticky1/2/3, unsticky, digest, undigest, lock, unlock, delete
func (s *Store) AdminThreadAction(ctx context.Context, op string, tids []int64) ([]*Thread, error) {
	var out []*Thread
	for _, tid := range tids {
		switch op {
		case "sticky1", "sticky2", "sticky3":
			sticky := int(op[len(op)-1] - '0')
			if err := s.SetThreadProperties(ctx, tid, sticky, nil, nil); err != nil {
				return out, err
			}
		case "unsticky":
			if err := s.SetThreadProperties(ctx, tid, 0, nil, nil); err != nil {
				return out, err
			}
		case "digest":
			yes := true
			if err := s.SetThreadProperties(ctx, tid, -1, &yes, nil); err != nil {
				return out, err
			}
		case "undigest":
			no := false
			if err := s.SetThreadProperties(ctx, tid, -1, &no, nil); err != nil {
				return out, err
			}
		case "lock":
			yes := true
			if err := s.SetThreadProperties(ctx, tid, -1, nil, &yes); err != nil {
				return out, err
			}
		case "unlock":
			no := false
			if err := s.SetThreadProperties(ctx, tid, -1, nil, &no); err != nil {
				return out, err
			}
		case "delete":
			th0, err0 := s.Thread(ctx, tid)
			if errors.Is(err0, ErrNotFound) {
				continue
			}
			if err0 != nil {
				return out, err0
			}
			if _, _, err := s.DeletePost(ctx, th0.FirstPostID); err != nil {
				return out, err
			}
			out = append(out, th0) // 已删除，尾部统一查询会失败，这里直接带出快照供广播
		default:
			return out, errors.New("未知操作: " + op)
		}
		th, err := s.Thread(ctx, tid)
		if err == nil {
			out = append(out, th)
		}
	}
	return out, nil
}

// RecycleCount 回收站主题数（仪表盘待办）。
func (s *Store) RecycleCount(ctx context.Context) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted`).Scan(&n)
	return n
}

// ---- 用户管理 ----

// UserQuery 用户搜索。
type UserQuery struct {
	Keyword string // 用户名模糊
	Page    int
	Size    int
}

// SearchUsers 后台用户搜索（含禁言状态）。
func (s *Store) SearchUsers(ctx context.Context, q UserQuery) ([]*AdminUser, int, error) {
	w := ""
	var args []any
	if q.Keyword != "" {
		args = append(args, "%"+q.Keyword+"%")
		w = ` WHERE username ILIKE $1`
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, q.Size, (q.Page-1)*q.Size)
	rows, err := s.pool.Query(ctx,
		`SELECT id, username, email, group_id, post_count, created_at, coalesce(last_login_at, 'epoch'::timestamptz), banned_until, ban_reason, trust_level
		 FROM users`+w+` ORDER BY id LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2), qArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AdminUser
	for rows.Next() {
		var u AdminUser
		var bannedUntil *time.Time
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.GroupID, &u.PostCount,
			&u.CreatedAt, &u.LastLoginAt, &bannedUntil, &u.BanReason, &u.TrustLevel); err != nil {
			return nil, 0, err
		}
		if bannedUntil != nil && bannedUntil.After(time.Now()) {
			u.BannedUntil = *bannedUntil
			u.IsBanned = true
		}
		out = append(out, &u)
	}
	return out, total, rows.Err()
}

// AdminUser 后台用户行（含禁言状态）。
type AdminUser struct {
	ID          int64
	Username    string
	Email       string
	GroupID     int
	PostCount   int64
	CreatedAt   time.Time
	LastLoginAt time.Time
	BannedUntil time.Time
	BanReason   string
	IsBanned    bool
	TrustLevel  int
}

// TrustLevelName 信任等级显示名。
func (u *AdminUser) TrustLevelName() string { return TrustLevelName(u.TrustLevel) }

// BanUser 禁言（days<=0 表示永久）。
func (s *Store) BanUser(ctx context.Context, uid int64, days int, reason string) error {
	if days > 0 {
		_, err := s.pool.Exec(ctx,
			`UPDATE users SET banned_until = now() + make_interval(days => $2), ban_reason=$3 WHERE id=$1`,
			uid, days, reason)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET banned_until = 'infinity', ban_reason=$2 WHERE id=$1`, uid, reason)
	return err
}

// UnbanUser 解除禁言。
func (s *Store) UnbanUser(ctx context.Context, uid int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET banned_until = NULL, ban_reason='' WHERE id=$1`, uid)
	return err
}

// SetUserGroup 修改用户组（0=会员 1=管理员）。
func (s *Store) SetUserGroup(ctx context.Context, uid int64, groupID int) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET group_id=$2 WHERE id=$1`, uid, groupID)
	return err
}

// DeleteUser 删号：公开内容仍在则拒绝；仅剩软删楼层/主题时先硬删这些残留再删用户。
// 会话、草稿、点赞、通知等已 ON DELETE CASCADE。
func (s *Store) DeleteUser(ctx context.Context, uid int64) error {
	var n int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, uid).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM posts WHERE author_id=$1 AND NOT deleted`, uid).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrUserHasContent
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM threads WHERE author_id=$1 AND NOT deleted`, uid).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrUserHasContent
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM posts WHERE author_id=$1 AND deleted`, uid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE author_id=$1 AND deleted`, uid); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, uid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// BannedCount 当前禁言中的用户数（仪表盘）。
func (s *Store) BannedCount(ctx context.Context) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE banned_until IS NOT NULL AND banned_until > now()`).Scan(&n)
	return n
}

// IsBanned 用户是否在禁言期。
func (s *Store) IsBanned(ctx context.Context, uid int64) (bool, time.Time, string) {
	var until *time.Time
	var reason string
	err := s.pool.QueryRow(ctx,
		`SELECT banned_until, ban_reason FROM users WHERE id=$1`, uid).Scan(&until, &reason)
	if err != nil || until == nil || !until.After(time.Now()) {
		return false, time.Time{}, ""
	}
	return true, *until, reason
}

// DBSize 当前数据库体积（仪表盘）。
func (s *Store) DBSize(ctx context.Context) string {
	var sz string
	_ = s.pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_database_size(current_database()))`).Scan(&sz)
	return sz
}

// SchemaVersion 当前 schema_migrations 版本（健康检查用）。
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.pool.QueryRow(ctx, `SELECT coalesce(max(version),0) FROM schema_migrations`).Scan(&v)
	return v, err
}
