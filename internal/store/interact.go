// SPDX-License-Identifier: AGPL-3.0-or-later
// store/interact.go：点赞（统一动作表+计数回写）与服务端草稿。
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- 点赞（点赞等统一动作表：计数冗余回写）----

// LikeToggle 切换点赞，返回 (是否已赞, 最新计数)。
func (s *Store) LikeToggle(ctx context.Context, pid, uid int64) (bool, int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM post_actions WHERE pid=$1 AND uid=$2 AND action=1)`, pid, uid).Scan(&exists); err != nil {
		return false, 0, err
	}
	if exists {
		if _, err := tx.Exec(ctx, `DELETE FROM post_actions WHERE pid=$1 AND uid=$2 AND action=1`, pid, uid); err != nil {
			return false, 0, err
		}
	} else {
		if _, err := tx.Exec(ctx, `INSERT INTO post_actions (pid, uid, action) VALUES ($1,$2,1)`, pid, uid); err != nil {
			return false, 0, err
		}
	}
	var count int
	if err := tx.QueryRow(ctx,
		`UPDATE posts SET like_count = (
			SELECT count(*) FROM post_actions WHERE pid=$1 AND action=1)
		 WHERE id=$1 RETURNING like_count`, pid).Scan(&count); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return !exists, count, nil
}

// Likers 点赞用户名单（浮层展示用）。
func (s *Store) Likers(ctx context.Context, pid int64) ([]Liker, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.username FROM post_actions pa
		 JOIN users u ON u.id = pa.uid
		 WHERE pa.pid=$1 AND pa.action=1 ORDER BY pa.created_at`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Liker
	for rows.Next() {
		var l Liker
		if err := rows.Scan(&l.UID, &l.Name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Liker 点赞用户。
type Liker struct {
	UID  int64  `json:"uid"`
	Name string `json:"name"`
}

// ---- 服务端草稿（服务端草稿）----

// SaveDraft 保存（或清空）某个上下文的草稿；content 为空则删除记录。
func (s *Store) SaveDraft(ctx context.Context, uid int64, context, content string) error {
	if content == "" {
		_, err := s.pool.Exec(ctx, `DELETE FROM drafts WHERE user_id=$1 AND context=$2`, uid, context)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO drafts (user_id, context, content, updated_at) VALUES ($1,$2,$3,now())
		 ON CONFLICT (user_id, context) DO UPDATE SET content=EXCLUDED.content, updated_at=now()`,
		uid, context, content)
	return err
}

// Draft 读取草稿；不存在返回空串。
func (s *Store) Draft(ctx context.Context, uid int64, context string) (string, time.Time, error) {
	var content string
	var updated time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT content, updated_at FROM drafts WHERE user_id=$1 AND context=$2`, uid, context).
		Scan(&content, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, nil
	}
	return content, updated, err
}
