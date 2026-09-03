// SPDX-License-Identifier: AGPL-3.0-or-later
// store/forum.go：版块/分类/主题/楼层的读取查询（列表页与帖子页）。
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ---- 版块与分类 ----

const forumCols = `id, category_id, name, description,
	 thread_count, post_count, today_count,
	 coalesce(last_post_at, 'epoch'::timestamptz), last_post_at IS NOT NULL,
	 coalesce(last_post_uid,0), coalesce(last_post_author,''),
	 coalesce(last_thread_id,0), coalesce(last_thread_title,''), coalesce(moderators,'')`

func scanForum(row pgx.Row) (*Forum, error) {
	var f Forum
	err := row.Scan(&f.ID, &f.CategoryID, &f.Name, &f.Description,
		&f.ThreadCount, &f.PostCount, &f.TodayCount,
		&f.LastPostAt, &f.HasLastPost,
		&f.LastPostUID, &f.LastPostAuthor,
		&f.LastThreadID, &f.LastThreadTitle, &f.Moderators)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
}

// CategoriesWithForums 首页数据：全部分类及其版块。
func (s *Store) CategoriesWithForums(ctx context.Context) ([]*Category, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+forumCols+` FROM forums ORDER BY category_id, displayorder, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	catRows, err := s.pool.Query(ctx, `SELECT id, name FROM categories ORDER BY displayorder, id`)
	if err != nil {
		return nil, err
	}
	defer catRows.Close()

	byID := make(map[int]*Category)
	var cats []*Category
	for catRows.Next() {
		var c Category
		if err := catRows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		byID[c.ID] = &c
		cats = append(cats, &c)
	}

	for rows.Next() {
		f, err := scanForum(rows)
		if err != nil {
			return nil, err
		}
		if c, ok := byID[f.CategoryID]; ok {
			c.Forums = append(c.Forums, f)
		}
	}
	return cats, rows.Err()
}

func (s *Store) Forum(ctx context.Context, id int64) (*Forum, error) {
	return scanForum(s.pool.QueryRow(ctx, `SELECT `+forumCols+` FROM forums WHERE id=$1`, id))
}

// ---- 主题列表 ----

const threadCols = `t.id, t.forum_id, t.author_id, u.username, t.title,
	t.sticky, t.digest, t.closed, t.post_count, t.view_count,
	t.created_at, t.last_post_at, coalesce(t.last_post_uid,0), coalesce(lu.username,''), coalesce(t.first_post_id,0), t.pending, t.pending_reason`

const threadJoins = `FROM threads t
	JOIN users u  ON u.id  = t.author_id
	LEFT JOIN users lu ON lu.id = t.last_post_uid`

func (s *Store) Thread(ctx context.Context, id int64) (*Thread, error) {
	var t Thread
	err := s.pool.QueryRow(ctx,
		`SELECT `+threadCols+` `+threadJoins+` WHERE t.id=$1 AND NOT t.deleted`, id).
		Scan(&t.ID, &t.ForumID, &t.AuthorID, &t.AuthorName, &t.Title,
			&t.Sticky, &t.Digest, &t.Closed, &t.PostCount, &t.ViewCount,
			&t.CreatedAt, &t.LastPostAt, &t.LastPostUID, &t.LastPostName, &t.FirstPostID, &t.Pending, &t.PendingReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// Stickies 版块置顶帖（每页顶部都显示，与 经典论坛的通行行为）。
func (s *Store) Stickies(ctx context.Context, forumID int64) ([]*Thread, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+threadCols+` `+threadJoins+`
		 WHERE t.forum_id=$1 AND NOT t.deleted AND NOT t.pending AND t.sticky > 0
		 ORDER BY t.sticky DESC, t.last_post_at DESC`, forumID)
	return collectThreads(rows, err)
}

// Threads 版块普通主题分页。
func (s *Store) Threads(ctx context.Context, forumID int64, page, size int) ([]*Thread, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM threads WHERE forum_id=$1 AND NOT deleted AND sticky=0`, forumID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+threadCols+` `+threadJoins+`
		 WHERE t.forum_id=$1 AND NOT t.deleted AND NOT t.pending AND t.sticky=0
		 ORDER BY t.last_post_at DESC LIMIT $2 OFFSET $3`, forumID, size, (page-1)*size)
	list, err := collectThreads(rows, err)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// RecentThreads 用户主页：最近参与的主题。
func (s *Store) RecentThreadsOfUser(ctx context.Context, uid int64, limit int) ([]*Thread, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+threadCols+` `+threadJoins+`
		 WHERE t.author_id=$1 AND NOT t.deleted AND NOT t.pending
		 ORDER BY t.last_post_at DESC LIMIT $2`, uid, limit)
	return collectThreads(rows, err)
}

func collectThreads(rows pgx.Rows, err error) ([]*Thread, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Thread
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.ForumID, &t.AuthorID, &t.AuthorName, &t.Title,
			&t.Sticky, &t.Digest, &t.Closed, &t.PostCount, &t.ViewCount,
			&t.CreatedAt, &t.LastPostAt, &t.LastPostUID, &t.LastPostName, &t.FirstPostID, &t.Pending, &t.PendingReason); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// ---- 楼层 ----

const postCols = `p.id, p.thread_id, p.author_id, u.username, u.group_id,
	p.floor, p.content_md, p.content_html, p.created_at,
	coalesce(p.edited_at, 'epoch'::timestamptz), p.edited_at IS NOT NULL, p.pending, p.pending_reason, p.like_count, p.version`

const postJoins = `FROM posts p JOIN users u ON u.id = p.author_id`

// Posts 帖子页楼层分页（楼层升序）；includePending 控制是否包含待审核楼层
//（作者与管理员/版主可见，普通访客不可见）。
func (s *Store) Posts(ctx context.Context, threadID int64, page, size int, includePending bool) ([]*Post, error) {
	pendingFilter := " AND NOT p.pending"
	if includePending {
		pendingFilter = ""
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+postCols+` `+postJoins+`
		 WHERE p.thread_id=$1 AND NOT p.deleted`+pendingFilter+`
		 ORDER BY p.floor LIMIT $2 OFFSET $3`, threadID, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.AuthorID, &p.AuthorName, &p.AuthorGroup,
			&p.Floor, &p.ContentMD, &p.ContentHTML, &p.CreatedAt, &p.EditedAt, &p.HasEdited, &p.Pending, &p.PendingReason, &p.LikeCount, &p.Version); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) Post(ctx context.Context, id int64) (*Post, error) {
	var p Post
	err := s.pool.QueryRow(ctx,
		`SELECT `+postCols+` `+postJoins+` WHERE p.id=$1 AND NOT p.deleted`, id).
		Scan(&p.ID, &p.ThreadID, &p.AuthorID, &p.AuthorName, &p.AuthorGroup,
			&p.Floor, &p.ContentMD, &p.ContentHTML, &p.CreatedAt, &p.EditedAt, &p.HasEdited, &p.Pending, &p.PendingReason, &p.LikeCount, &p.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}
