// SPDX-License-Identifier: AGPL-3.0-or-later
// store/community.go：收藏/订阅、编辑历史、简单声望（ROADMAP P2 批次）。
package store

import (
	"context"
	"time"
)

// ---- 收藏 / 订阅 ----

// FavoriteToggle 收藏/取消收藏主题，返回收藏后的状态。
func (s *Store) FavoriteToggle(ctx context.Context, uid, tid int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM thread_favorites WHERE user_id=$1 AND thread_id=$2`, uid, tid)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil // 原已收藏，本次为取消
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO thread_favorites (user_id, thread_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		uid, tid)
	if err != nil {
		return false, err
	}
	return true, nil
}

// IsFavorite 用户是否已收藏主题。
func (s *Store) IsFavorite(ctx context.Context, uid, tid int64) bool {
	var ok bool
	_ = s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM thread_favorites WHERE user_id=$1 AND thread_id=$2)`, uid, tid).Scan(&ok)
	return ok
}

// UnreadFavorites 收藏的主题中有新回复（楼层数超过已读进度）的数量。
func (s *Store) UnreadFavorites(ctx context.Context, uid int64) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx, `
		SELECT count(*) FROM thread_favorites f
		JOIN threads t ON t.id = f.thread_id AND NOT t.deleted AND NOT t.pending
		LEFT JOIN thread_reads r ON r.user_id = f.user_id AND r.thread_id = f.thread_id
		WHERE f.user_id=$1 AND t.post_count > coalesce(r.last_floor, 0)`, uid).Scan(&n)
	return n
}

// FavoriteRow 收藏列表条目（含未读进度）。
type FavoriteRow struct {
	Thread
	LastFloor int  // 已读进度（0 = 未读）
	HasNew    bool // 有新回复
}

// FavoritesOfUser 收藏列表分页（收藏时间倒序，公开口径）。
func (s *Store) FavoritesOfUser(ctx context.Context, uid int64, page, size int) ([]*FavoriteRow, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM thread_favorites f
		JOIN threads t ON t.id = f.thread_id AND NOT t.deleted AND NOT t.pending
		WHERE f.user_id=$1`, uid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+threadCols+`, coalesce(r.last_floor, 0), t.post_count > coalesce(r.last_floor, 0)
		FROM thread_favorites f
		JOIN threads t ON t.id = f.thread_id AND NOT t.deleted AND NOT t.pending
		JOIN users u ON u.id = t.author_id
		LEFT JOIN users lu ON lu.id = t.last_post_uid
		LEFT JOIN thread_reads r ON r.user_id = f.user_id AND r.thread_id = f.thread_id
		WHERE f.user_id=$1
		ORDER BY f.created_at DESC LIMIT $2 OFFSET $3`, uid, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*FavoriteRow
	for rows.Next() {
		var fr FavoriteRow
		if err := rows.Scan(&fr.ID, &fr.ForumID, &fr.AuthorID, &fr.AuthorName, &fr.Title,
			&fr.Sticky, &fr.Digest, &fr.Closed, &fr.PostCount, &fr.ViewCount,
			&fr.CreatedAt, &fr.LastPostAt, &fr.LastPostUID, &fr.LastPostName, &fr.FirstPostID,
			&fr.Pending, &fr.PendingReason, &fr.LastFloor, &fr.HasNew); err != nil {
			return nil, 0, err
		}
		out = append(out, &fr)
	}
	return out, total, rows.Err()
}

// ---- 编辑历史 ----

// PostEdit 楼层编辑历史条目（改前快照）。
type PostEdit struct {
	ID        int64
	EditorID  int64
	Editor    string
	ContentMD string
	CreatedAt time.Time
}

// SavePostEdit 保存编辑前快照（UpdatePost 前调用）。
func (s *Store) SavePostEdit(ctx context.Context, postID, editorID int64, contentMD string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO post_edits (post_id, editor_id, content_md) VALUES ($1,$2,$3)`,
		postID, editorID, contentMD)
	return err
}

// PostEditsOf 楼层编辑历史（最新在前）。
func (s *Store) PostEditsOf(ctx context.Context, postID int64) ([]*PostEdit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.editor_id, coalesce(u.username, '已注销'), e.content_md, e.created_at
		FROM post_edits e LEFT JOIN users u ON u.id = e.editor_id
		WHERE e.post_id=$1 ORDER BY e.id DESC LIMIT 20`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PostEdit
	for rows.Next() {
		var e PostEdit
		if err := rows.Scan(&e.ID, &e.EditorID, &e.Editor, &e.ContentMD, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// PostEditCount 楼层编辑次数（0 = 无历史，隐藏「历史」入口）。
func (s *Store) PostEditCount(ctx context.Context, postID int64) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM post_edits WHERE post_id=$1`, postID).Scan(&n)
	return n
}

// ---- 简单声望 ----

// Reputation 用户声望：公开发帖数 + 获赞数。
func (s *Store) Reputation(ctx context.Context, uid int64) (int64, int64, error) {
	var posts, likes int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM posts WHERE author_id=$1 AND NOT deleted AND NOT pending`, uid).Scan(&posts); err != nil {
		return 0, 0, err
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM post_actions pa
		JOIN posts p ON p.id = pa.pid
		WHERE p.author_id=$1`, uid).Scan(&likes); err != nil {
		return 0, 0, err
	}
	return posts, likes, nil
}

// ---- 草稿箱 ----

// DraftRow 草稿箱条目。
type DraftRow struct {
	Context   string
	Content   string
	UpdatedAt time.Time
}

// DraftsOfUser 用户全部非空草稿（更新时间倒序）。
func (s *Store) DraftsOfUser(ctx context.Context, uid int64) ([]*DraftRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT context, content, updated_at FROM drafts
		WHERE user_id=$1 AND content <> '' ORDER BY updated_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DraftRow
	for rows.Next() {
		var d DraftRow
		if err := rows.Scan(&d.Context, &d.Content, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// DeleteDraft 删除指定草稿（草稿箱删除按钮）。
func (s *Store) DeleteDraft(ctx context.Context, uid int64, context string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM drafts WHERE user_id=$1 AND context=$2`, uid, context)
	return err
}

// DraftContextLabel 草稿上下文的展示信息（页面 + 跳转链接）。
func DraftContextLabel(c string) (label, link string) {
	switch {
	case len(c) > 4 && c[:4] == "new:":
		return "新主题（版块 #" + c[4:] + "）", "/new?fid=" + c[4:]
	case len(c) > 6 && c[:6] == "reply:":
		return "回复主题 #" + c[6:], "/thread-" + c[6:] + "-1-1.html"
	case len(c) > 5 && c[:5] == "edit:":
		return "编辑楼层 #" + c[5:], "/post/" + c[5:] + "/history"
	}
	return c, "/"
}
