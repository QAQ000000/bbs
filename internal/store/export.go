// SPDX-License-Identifier: AGPL-3.0-or-later
// store/export.go：个人数据导出（ROADMAP 6.3）。只含本人内容，绝不包含密码哈希。
package store

import (
	"context"
	"time"
)

// ExportThread 导出的主题行（本人创建）。
type ExportThread struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// ExportPost 导出的楼层行（含所属主题标题；不含已删除楼层）。
type ExportPost struct {
	ThreadID    int64     `json:"thread_id"`
	ThreadTitle string    `json:"thread_title"`
	Floor       int       `json:"floor"`
	ContentMD   string    `json:"content_md"`
	CreatedAt   time.Time `json:"created_at"`
}

// ExportThreadsOfUser 本人创建的主题（含待审核与回收站中的，均属本人数据）。
func (s *Store) ExportThreadsOfUser(ctx context.Context, uid int64) ([]*ExportThread, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, created_at FROM threads
		WHERE author_id=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ExportThread
	for rows.Next() {
		var t ExportThread
		if err := rows.Scan(&t.ID, &t.Title, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// ExportPostsOfUser 本人发布的全部未删除楼层（含待审核）。
func (s *Store) ExportPostsOfUser(ctx context.Context, uid int64) ([]*ExportPost, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.thread_id, t.title, p.floor, p.content_md, p.created_at
		FROM posts p JOIN threads t ON t.id = p.thread_id
		WHERE p.author_id=$1 AND NOT p.deleted
		ORDER BY p.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ExportPost
	for rows.Next() {
		var p ExportPost
		if err := rows.Scan(&p.ThreadID, &p.ThreadTitle, &p.Floor, &p.ContentMD, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}
