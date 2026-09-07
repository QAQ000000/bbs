// SPDX-License-Identifier: AGPL-3.0-or-later
// store/report.go：举报（ROADMAP 阶段三）。会员举报楼层，staff 在审核队列处理。
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dzforum/internal/perm"
	"github.com/jackc/pgx/v5"
)

// ReportStatus 举报状态。
const (
	ReportOpen      = "open"
	ReportResolved  = "resolved"  // 已处理（如删除楼层）
	ReportDismissed = "dismissed" // 驳回（内容无问题）
)

// ReportRow 审核队列中的举报行（含被举报楼层上下文）。
type ReportRow struct {
	ID         int64
	PostID     int64
	ReporterID int64
	Reporter   string
	Reason     string
	CreatedAt  time.Time

	Excerpt    string
	Floor      int
	Pending    bool
	Deleted    bool
	TID        int64
	ThreadTtl  string
	AuthorName string
}

// CreateReport 提交举报；同人对同一楼层的未处理举报唯一（索引兜底，重复提交静默忽略）。
func (s *Store) CreateReport(ctx context.Context, postID, reporterUID int64, reason string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reports (post_id, reporter, reason) VALUES ($1,$2,$3)
		 ON CONFLICT (post_id, reporter) WHERE status = 'open' DO NOTHING`,
		postID, reporterUID, reason)
	return err
}

// OpenReports 待处理举报列表；forumIDs 非空时限定版主管辖范围。
func (s *Store) OpenReports(ctx context.Context, limit int, forumIDs []int64) ([]*ReportRow, error) {
	var rows pgx.Rows
	var err error
	if len(forumIDs) > 0 {
		rows, err = s.pool.Query(ctx, `
			SELECT r.id, r.post_id, r.reporter, ru.username, r.reason, r.created_at,
			       left(p.content_md, 120), p.floor, p.pending, p.deleted,
			       t.id, t.title, pu.username
			FROM reports r
			JOIN posts p   ON p.id  = r.post_id
			JOIN threads t ON t.id  = p.thread_id
			JOIN users pu  ON pu.id = p.author_id
			JOIN users ru  ON ru.id = r.reporter
			WHERE r.status = 'open' AND t.forum_id = ANY($1)
			ORDER BY r.id DESC LIMIT $2`, forumIDs, limit)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT r.id, r.post_id, r.reporter, ru.username, r.reason, r.created_at,
			       left(p.content_md, 120), p.floor, p.pending, p.deleted,
			       t.id, t.title, pu.username
			FROM reports r
			JOIN posts p   ON p.id  = r.post_id
			JOIN threads t ON t.id  = p.thread_id
			JOIN users pu  ON pu.id = p.author_id
			JOIN users ru  ON ru.id = r.reporter
			WHERE r.status = 'open'
			ORDER BY r.id DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReportRow
	for rows.Next() {
		var rp ReportRow
		if err := rows.Scan(&rp.ID, &rp.PostID, &rp.ReporterID, &rp.Reporter, &rp.Reason, &rp.CreatedAt,
			&rp.Excerpt, &rp.Floor, &rp.Pending, &rp.Deleted,
			&rp.TID, &rp.ThreadTtl, &rp.AuthorName); err != nil {
			return nil, err
		}
		out = append(out, &rp)
	}
	return out, rows.Err()
}

// OpenReportCount 待处理举报数（仪表盘/队列标题）。
func (s *Store) OpenReportCount(ctx context.Context) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM reports WHERE status = 'open'`).Scan(&n)
	return n
}

// ErrReportNotFound 举报不存在或已被处理。
var ErrReportNotFound = errors.New("report not found")
var ErrReportForbidden = errors.New("report outside moderation scope")

// HandleReport commits the content action, result notification and audit together.
// Already deleted content is a successful no-op; a closed report cannot be handled twice.
func (s *Store) HandleReport(ctx context.Context, reportID, actor int64, action, ip string) (postID, tid int64, floor int, deleted bool, err error) {
	if action != "delete" && action != "dismiss" {
		return 0, 0, 0, false, errors.New("invalid report action")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	var fid int64
	err = tx.QueryRow(ctx, `SELECT t.id,t.forum_id FROM threads t JOIN posts p ON p.thread_id=t.id JOIN reports r ON r.post_id=p.id WHERE r.id=$1 FOR UPDATE OF t`, reportID).Scan(&tid, &fid)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrReportNotFound
	}
	if err != nil {
		return
	}
	if err = lockForumStats(ctx, tx, fid); err != nil {
		return
	}
	var role perm.Role
	var username string
	err = tx.QueryRow(ctx, `SELECT group_id,username FROM users WHERE id=$1 AND NOT coalesce(blocked_until>now(),false)`, actor).Scan(&role, &username)
	if err != nil {
		return
	}
	if !perm.Allowed(role, perm.ContentModerate) || (action == "delete" && !perm.Allowed(role, perm.ContentDeleteAny)) {
		err = ErrReportForbidden
		return
	}
	if !perm.Allowed(role, perm.AdminPanel) {
		var id int64
		err = tx.QueryRow(ctx, `SELECT forum_id FROM forum_moderators WHERE user_id=$1 AND forum_id=$2 FOR SHARE`, actor, fid).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrReportForbidden
		}
		if err != nil {
			return
		}
	}
	err = tx.QueryRow(ctx, `SELECT r.post_id,p.floor,p.deleted OR t.deleted FROM reports r JOIN posts p ON p.id=r.post_id JOIN threads t ON t.id=p.thread_id WHERE r.id=$1 AND r.status='open' FOR UPDATE OF r,p`, reportID).Scan(&postID, &floor, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrReportNotFound
	}
	if err != nil {
		return
	}
	status := ReportDismissed
	if action == "delete" {
		status = ReportResolved
		if !deleted {
			_, _, err = deletePostTx(ctx, tx, postID, "")
			if err != nil {
				return
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE reports SET status=$2,handled_by=$3,handled_at=now() WHERE id=$1`, reportID, status, actor)
	if err != nil {
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail,ip) VALUES($1,$2,$3,$4,$5)`, actor, username, "report."+action, fmt.Sprintf("report=%d post=%d already_deleted=%t", reportID, postID, deleted), ip)
	if err != nil {
		return
	}
	err = tx.Commit(ctx)
	return
}

// ReportThreadID 举报对应的主题 id（管辖范围校验用）。
func (s *Store) ReportThreadID(ctx context.Context, reportID int64) (int64, error) {
	var tid int64
	err := s.pool.QueryRow(ctx, `
		SELECT p.thread_id FROM reports r JOIN posts p ON p.id = r.post_id
		WHERE r.id = $1 AND r.status = 'open'`, reportID).Scan(&tid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrReportNotFound
	}
	return tid, err
}
