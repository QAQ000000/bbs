// SPDX-License-Identifier: AGPL-3.0-or-later
// store/write.go：写操作事务（发主题/回帖/编辑/删除），维护反范式计数一致性。
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ---- 写操作（事务）----

// bumpForumSQL 维护版块反范式统计：总帖数、今日帖数（跨天自动归零）、最后发表。
// $1 版块, $2 用户, $3 用户名, $4 主题id, $5 主题标题, $6 是否同时 +主题数
const bumpForumSQL = `UPDATE forums SET
	post_count = post_count + 1,
	last_post_at = now(),
	last_post_uid = $2,
	last_post_author = $3,
	last_thread_id = $4,
	last_thread_title = $5,
	thread_count = thread_count + CASE WHEN $6 THEN 1 ELSE 0 END
	WHERE id = $1`

// CreateThread 发新主题：建主题 + 首楼 + 统计，返回主题与首楼。
// pending=true 时主题进入审核队列（公开列表不可见，作者与管理人员可见）。
func (s *Store) CreateThread(ctx context.Context, forumID, authorID int64, authorName, title, md, html string, pending bool, reason string) (*Thread, *Post, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	var tid int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO threads (forum_id, author_id, title, pending, pending_reason) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		forumID, authorID, title, pending, reason).Scan(&tid); err != nil {
		return nil, nil, err
	}

	var pid int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO posts (thread_id, author_id, floor, content_md, content_html, pending, pending_reason)
		 VALUES ($1,$2,1,$3,$4,$5,$6) RETURNING id`, tid, authorID, md, html, pending, reason).Scan(&pid); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE threads SET first_post_id=$1, post_count=1 WHERE id=$2`, pid, tid); err != nil {
		return nil, nil, err
	}
	if !pending {
		if _, err := tx.Exec(ctx, bumpForumSQL, forumID, authorID, authorName, tid, title, true); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET post_count=post_count+1 WHERE id=$1`, authorID); err != nil {
			return nil, nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	markMemberCommit(ctx)
	_ = s.IndexPost(ctx, pid, title, md) // 首楼：标题权重 A + 正文 B
	th, err := s.Thread(ctx, tid)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.Post(ctx, pid)
	return th, p, err
}

// CreateReply 回帖：楼层号单调递增（行锁保证并发不重号），
// 返回新楼层与更新后的主题。
func (s *Store) CreateReply(ctx context.Context, threadID, authorID int64, authorName, md, html string, pending bool, reason string) (*Thread, *Post, error) {
	return s.CreateReplyTo(ctx, threadID, authorID, authorName, md, html, pending, reason, 0)
}

func (s *Store) CreateReplyTo(ctx context.Context, threadID, authorID int64, authorName, md, html string, pending bool, reason string, replyTo int64) (*Thread, *Post, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	var th Thread
	var floor int
	err = tx.QueryRow(ctx,
		`UPDATE threads SET post_count=post_count+1, floor_seq=floor_seq+1,
			last_post_at = CASE WHEN $3::bool THEN last_post_at ELSE now() END,
			last_post_uid = CASE WHEN $3::bool THEN last_post_uid ELSE $2 END
		 WHERE id=$1 AND NOT deleted AND NOT closed
			 RETURNING id, forum_id, author_id, title, sticky, digest, closed, post_count, view_count, created_at, last_post_at, coalesce(last_post_uid,0), floor_seq, pending`,
		threadID, authorID, pending).
		Scan(&th.ID, &th.ForumID, &th.AuthorID, &th.Title, &th.Sticky, &th.Digest, &th.Closed,
			&th.PostCount, &th.ViewCount, &th.CreatedAt, &th.LastPostAt, &th.LastPostUID, &floor, &th.Pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	// floor 已从 floor_seq（单调楼层序号）取回：删除中间楼层后的新回复
	// 不再复用已删除楼层号，避免出现重复公开楼层
	if replyTo != 0 {
		var target int64
		err = tx.QueryRow(ctx, `SELECT id FROM posts WHERE id=$1 AND thread_id=$2 AND NOT pending AND NOT deleted FOR SHARE`, replyTo, threadID).Scan(&target)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		if err != nil {
			return nil, nil, err
		}
	}
	var pid int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO posts (thread_id, author_id, floor, content_md, content_html, pending, pending_reason,reply_to_post_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,nullif($8,0)) RETURNING id`, threadID, authorID, floor, md, html, pending, reason, replyTo).Scan(&pid); err != nil {
		return nil, nil, err
	}
	var forum Forum
	if err := tx.QueryRow(ctx, `SELECT name FROM forums WHERE id=$1`, th.ForumID).Scan(&forum.Name); err != nil {
		return nil, nil, err
	}
	if !pending {
		if _, err := tx.Exec(ctx, bumpForumSQL, th.ForumID, authorID, authorName, threadID, th.Title, false); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET post_count=post_count+1 WHERE id=$1`, authorID); err != nil {
			return nil, nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	markMemberCommit(ctx)
	th.LastPostUID = authorID
	th.LastPostName = authorName
	p, err := s.Post(ctx, pid)
	if err == nil {
		_ = s.IndexPost(ctx, pid, "", md) // 回复：仅正文
	}
	return &th, p, err
}

// ErrEditConflict 编辑冲突：提交所基于的版本已落后于当前版本。
var ErrEditConflict = errors.New("edit conflict")

// UpdatePost 编辑楼层内容；若为首楼且改了标题则同步主题标题。
// expectedVersion 为编辑表单携带的版本号（0=不校验）：行锁内校验版本，
// 并发编辑只有一个请求成功，后写不再覆盖先写。
// 改前快照与内容更新同事务：更新失败快照也不会落盘（历史不重复）。
func (s *Store) UpdatePost(ctx context.Context, postID int64, expectedVersion int, editorID int64, prevMD, title, md, html string) (*Post, *Thread, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	var floor int
	var tid int64
	var version int
	err = tx.QueryRow(ctx,
		`SELECT floor, thread_id, version FROM posts WHERE id=$1 FOR UPDATE`,
		postID).Scan(&floor, &tid, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if expectedVersion > 0 && version != expectedVersion {
		return nil, nil, ErrEditConflict
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO post_edits (post_id, editor_id, content_md) VALUES ($1,$2,$3)`,
		postID, editorID, prevMD); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE posts SET content_md=$2, content_html=$3, edited_at=now(), version=version+1 WHERE id=$1`,
		postID, md, html); err != nil {
		return nil, nil, err
	}

	if floor == 1 {
		if _, err := tx.Exec(ctx, `UPDATE threads SET title=$2 WHERE id=$1`, tid, title); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	p, err := s.Post(ctx, postID)
	if err != nil {
		return nil, nil, err
	}
	if p.Floor == 1 {
		_ = s.IndexPost(ctx, postID, thTitle(ctx, s, tid), md)
	} else {
		_ = s.IndexPost(ctx, postID, "", md)
	}
	th, err := s.Thread(ctx, tid)
	return p, th, err
}

// thTitle 查主题标题（索引用）。
func thTitle(ctx context.Context, s *Store, tid int64) string {
	var title string
	_ = s.pool.QueryRow(ctx, `SELECT title FROM threads WHERE id=$1`, tid).Scan(&title)
	return title
}

// DeletePost 删除楼层。若为首楼则整个主题软删；
// 否则递减计数并返回主题 id（供广播与跳转）。
func (s *Store) DeletePost(ctx context.Context, postID int64) (deletedThread bool, tid int64, err error) {
	return s.deletePost(ctx, postID, "")
}

func (s *Store) RejectPost(ctx context.Context, postID int64, note string) (bool, int64, error) {
	if note == "" {
		note = "未通过内容审核"
	}
	return s.deletePost(ctx, postID, note)
}

func (s *Store) deletePost(ctx context.Context, postID int64, note string) (deletedThread bool, tid int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)

	var uid, floor int
	var forumID int64
	// Lock the parent before posts, matching CreateReply's lock order.
	err = tx.QueryRow(ctx, `SELECT t.id, t.forum_id FROM threads t
		WHERE t.id=(SELECT thread_id FROM posts WHERE id=$1) AND NOT t.deleted
		FOR UPDATE`, postID).Scan(&tid, &forumID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, ErrNotFound
	}
	if err != nil {
		return false, 0, err
	}
	var pending bool
	err = tx.QueryRow(ctx, `SELECT author_id, floor, pending FROM posts WHERE id=$1 AND NOT deleted FOR UPDATE`, postID).
		Scan(&uid, &floor, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, ErrNotFound
	}
	if err != nil {
		return false, 0, err
	}

	if floor == 1 {
		if note != "" {
			if _, err := tx.Exec(ctx, `UPDATE posts SET moderation_status='rejected',moderation_note=$2 WHERE thread_id=$1 AND pending AND NOT deleted`, tid, note); err != nil {
				return false, 0, err
			}
		}
		var fID int64
		if err := tx.QueryRow(ctx, `SELECT forum_id FROM threads WHERE id=$1`, tid).Scan(&fID); err != nil {
			return false, 0, err
		}
		forumID = fID
		if _, err := tx.Exec(ctx, `UPDATE threads SET deleted=true WHERE id=$1`, tid); err != nil {
			return false, 0, err
		}
		// 随主题删除的楼层打批次标记：恢复主题时只恢复这批，
		// 此前被单独删除的楼层保持隐藏，不重新公开
		if _, err := tx.Exec(ctx,
			`UPDATE posts SET deleted=true, thread_deleted=true WHERE thread_id=$1 AND NOT deleted`, tid); err != nil {
			return false, 0, err
		}
		// 回补各作者的发帖计数（只算这次随主题删除的公开楼层）
		if _, err := tx.Exec(ctx, `
			UPDATE users u SET post_count = GREATEST(u.post_count - x.n, 0)
			FROM (SELECT author_id, count(*) AS n FROM posts
			      WHERE thread_id=$1 AND deleted AND thread_deleted AND NOT pending
			      GROUP BY author_id) x
			WHERE u.id = x.author_id`, tid); err != nil {
			return false, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		_ = s.RecomputeForumStats(ctx, forumID)
		return true, tid, nil
	}

	if note != "" && pending {
		if _, err := tx.Exec(ctx, `UPDATE posts SET moderation_status='rejected',moderation_note=$2 WHERE id=$1`, postID, note); err != nil {
			return false, 0, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE posts SET deleted=true WHERE id=$1`, postID); err != nil {
		return false, 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET post_count = GREATEST(post_count-1, 0) WHERE id=$1 AND NOT $2::bool`, uid, pending); err != nil {
		return false, 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE threads SET post_count=GREATEST(post_count-1, 0) WHERE id=$1`, tid); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	var forumID2 int64
	if err := s.pool.QueryRow(ctx, `SELECT forum_id FROM threads WHERE id=$1`, tid).Scan(&forumID2); err == nil {
		_ = s.RecomputeForumStats(ctx, forumID2)
	}
	return false, tid, nil
}

// SetThreadProperties 置顶/加精/锁定（管理员）。
func (s *Store) SetThreadProperties(ctx context.Context, tid int64, sticky int, digest, closed *bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE threads SET
		sticky = CASE WHEN $2 >= 0 THEN $2 ELSE sticky END,
		digest = coalesce($3, digest),
		closed = coalesce($4, closed)
		WHERE id=$1`, tid, sticky, digest, closed)
	return err
}

// SetPostIP 记录楼层发布来源 IP（隐私政策声明；仅管理员可见掩码）。
// 首次写入后不再覆盖（编辑不改来源）。
func (s *Store) SetPostIP(ctx context.Context, postID int64, ip string) {
	if ip == "" {
		return
	}
	_, _ = s.pool.Exec(ctx, `UPDATE posts SET ip=$2 WHERE id=$1 AND ip=''`, postID, ip)
}
