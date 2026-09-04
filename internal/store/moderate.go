// SPDX-License-Identifier: AGPL-3.0-or-later
// store/moderate.go：发帖审核队列与批量删帖（事务内重算计数）、版主管辖范围。
package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// ---- 发帖审核（阶段三）----

// SetThreadApproved 审核通过主题（连同其待审核楼层），
// 待审核内容自此进入公开口径：回补作者计数并重算主题/版块统计。
func (s *Store) SetThreadApproved(ctx context.Context, tid int64) error {
	authors, err := s.ApproveThreadPendingAuthors(ctx, tid)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE threads SET pending=false WHERE id=$1`, tid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE posts SET pending=false WHERE thread_id=$1 AND pending`, tid); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, a := range authors {
		_ = s.BumpUsersPostCount(ctx, a.UID, int64(a.Count))
	}
	if err := s.RecomputeThreadLastPost(ctx, tid); err != nil {
		return err
	}
	var fid int64
	if err := s.pool.QueryRow(ctx, `SELECT forum_id FROM threads WHERE id=$1`, tid).Scan(&fid); err != nil {
		return err
	}
	return s.RecomputeForumStats(ctx, fid)
}

// SetPostPendingModeration 将楼层（首楼连主题）标记为待审核并记录原因。
// 编辑复检使用：过审内容被重新编辑时按规则重新入队。
func (s *Store) SetPostPendingModeration(ctx context.Context, postID int64, reason string) error {
	var tid int64
	var floor int
	if err := s.pool.QueryRow(ctx,
		`UPDATE posts SET pending=true, pending_reason=$2 WHERE id=$1 AND NOT deleted
		 RETURNING thread_id, floor`, postID, reason).Scan(&tid, &floor); err != nil {
		return err
	}
	if floor == 1 {
		_, err := s.pool.Exec(ctx,
			`UPDATE threads SET pending=true, pending_reason=$2 WHERE id=$1`, tid, reason)
		return err
	}
	// 非首楼重新入队：主题活跃度回退到最新公开楼层
	_ = s.RecomputeThreadLastPost(ctx, tid)
	return nil
}

func (s *Store) SetPostApproved(ctx context.Context, pid int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE posts SET pending=false WHERE id=$1`, pid)
	return err
}

// PendingThreadRow 审核队列中的主题行（含首楼内容摘要）。
type PendingThreadRow struct {
	Thread  Thread
	Excerpt string
}

// PendingThreads 待审核主题列表；forumIDs 非空时限定版主管辖范围。
func (s *Store) PendingThreads(ctx context.Context, limit int, forumIDs []int64) ([]*PendingThreadRow, error) {
	scope := ""
	args := []any{}
	if len(forumIDs) > 0 {
		args = append(args, forumIDs)
		scope = ` AND t.forum_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+threadCols+`, left(p.content_md, 200) `+threadJoins+`
		  JOIN posts p ON p.thread_id = t.id AND p.floor = 1
		 WHERE NOT t.deleted AND t.pending`+scope+`
		 ORDER BY t.created_at DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*PendingThreadRow
	for rows.Next() {
		var r PendingThreadRow
		t := &r.Thread
		if err := rows.Scan(&t.ID, &t.ForumID, &t.AuthorID, &t.AuthorName, &t.Title,
			&t.Sticky, &t.Digest, &t.Closed, &t.PostCount, &t.ViewCount,
			&t.CreatedAt, &t.LastPostAt, &t.LastPostUID, &t.LastPostName, &t.FirstPostID, &t.Pending, &t.PendingReason,
			&r.Excerpt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// PendingPostRow 审核队列中的回复行（不含待审主题内的楼层）。
type PendingPostRow struct {
	Post      Post
	ThreadID  int64
	ThreadTtl string
}

func (s *Store) PendingPosts(ctx context.Context, limit int, forumIDs []int64) ([]*PendingPostRow, error) {
	scope := ""
	args := []any{}
	if len(forumIDs) > 0 {
		args = append(args, forumIDs)
		scope = ` AND t.forum_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+postCols+`, t.id, t.title `+postJoins+`
		  JOIN threads t ON t.id = p.thread_id AND NOT t.pending
		 WHERE NOT p.deleted AND p.pending`+scope+`
		 ORDER BY p.id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PendingPostRow
	for rows.Next() {
		var r PendingPostRow
		p := &r.Post
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.AuthorID, &p.AuthorName, &p.AuthorGroup,
			&p.Floor, &p.ContentMD, &p.ContentHTML, &p.CreatedAt, &p.EditedAt, &p.HasEdited, &p.Pending, &p.PendingReason, &p.LikeCount, &p.Version,
			&r.ThreadID, &r.ThreadTtl); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// PendingCounts 待审核数量（仪表盘/菜单）。
func (s *Store) PendingCounts(ctx context.Context) (threads int64, posts int64) {
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted=false AND pending`).Scan(&threads)
	_ = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM posts p JOIN threads t ON t.id=p.thread_id AND NOT t.pending
		 WHERE NOT p.deleted AND p.pending`).Scan(&posts)
	return
}

// ---- 批量删帖（回复部分）----

// PrunePosts 按条件软删回复并重算计数，返回删除条数。
// 条件至少给一个：作者用户名 / 版块 / 删除 N 天前的楼层（仅限回帖，不动首楼）。
func (s *Store) PrunePosts(ctx context.Context, author string, forumID int64, beforeDays int) (int64, error) {
	if author == "" && forumID == 0 && beforeDays <= 0 {
		return 0, errors.New("拒绝无条件批量删除")
	}
	w := ` WHERE NOT p.deleted AND NOT p.pending AND p.floor > 1`
	var args []any
	if author != "" {
		args = append(args, author)
		w += ` AND u.username = $` + strconv.Itoa(len(args))
	}
	if forumID > 0 {
		args = append(args, forumID)
		w += ` AND t.forum_id = $` + strconv.Itoa(len(args))
	}
	if beforeDays > 0 {
		args = append(args, beforeDays)
		w += ` AND p.created_at < now() - make_interval(days => $` + strconv.Itoa(len(args)) + `)`
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var n int64
	var affectedThreadIDs []int64
	// 单条 CTE 链：定位目标 → 软删 → 差值递减计数。
	// 注意：同一语句内各 CTE 共享语句开始时的快照，重算不能依赖删除后的可见性，
	// 因此用 RETURNING 的删除行数做差值，而非重新 count。
	if err := tx.QueryRow(ctx, `
		WITH target AS (
			SELECT p.id, p.thread_id FROM posts p
			JOIN threads t ON t.id = p.thread_id
			JOIN users u ON u.id = p.author_id`+w+`
		), del AS (
			UPDATE posts SET deleted=true WHERE id IN (SELECT id FROM target)
			RETURNING thread_id, author_id
		), affected AS (
			SELECT thread_id, count(*) AS removed FROM del GROUP BY thread_id
		), upd_threads AS (
			UPDATE threads t SET post_count = GREATEST(t.post_count - a.removed, 0)
			FROM affected a WHERE t.id = a.thread_id
			RETURNING t.forum_id, a.removed
		), affected_users AS (
			SELECT author_id, count(*) AS removed FROM del GROUP BY author_id
		), upd_users AS (
			UPDATE users u SET post_count = GREATEST(u.post_count - a.removed, 0)
			FROM affected_users a WHERE u.id = a.author_id
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM del), COALESCE((SELECT array_agg(DISTINCT thread_id) FROM del), '{}')`, args...).Scan(&n, &affectedThreadIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	// 受影响版块的公开口径统计（含最后发表）统一重算
	for _, tid := range affectedThreadIDs {
		var fid int64
		if err := s.pool.QueryRow(ctx, `SELECT forum_id FROM threads WHERE id=$1`, tid).Scan(&fid); err == nil {
			_ = s.RecomputeForumStats(ctx, fid)
		}
	}
	return n, nil
}

// ---- 版主（阶段三）----

// ModeratorForumIDs 用户担任版主的版块 id 列表。
func (s *Store) ModeratorForumIDs(ctx context.Context, username string) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, moderators FROM forums WHERE moderators <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		var mods string
		if err := rows.Scan(&id, &mods); err != nil {
			return nil, err
		}
		for _, m := range strings.Split(mods, ",") {
			if strings.TrimSpace(m) == username {
				out = append(out, id)
				break
			}
		}
	}
	return out, rows.Err()
}
