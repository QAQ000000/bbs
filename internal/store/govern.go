// SPDX-License-Identifier: AGPL-3.0-or-later
// store/govern.go：回收站（恢复/彻底删除）、公告、敏感词过滤。
package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- 回收站（软删主题）----

// RecycleThreads 回收站主题分页；forumIDs 非空时限定版主管辖范围。
func (s *Store) RecycleThreads(ctx context.Context, page, size int, forumIDs []int64) ([]*Thread, int, error) {
	var total int
	var rows pgx.Rows
	var err error
	off := (page - 1) * size
	if len(forumIDs) > 0 {
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM threads t WHERE t.deleted AND t.forum_id = ANY($1)`, forumIDs).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err = s.pool.Query(ctx,
			`SELECT `+threadCols+` `+threadJoins+`
			 WHERE t.deleted AND t.forum_id = ANY($1) ORDER BY t.last_post_at DESC LIMIT $2 OFFSET $3`,
			forumIDs, size, off)
	} else {
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted`).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err = s.pool.Query(ctx,
			`SELECT `+threadCols+` `+threadJoins+`
			 WHERE t.deleted ORDER BY t.last_post_at DESC LIMIT $1 OFFSET $2`, size, off)
	}
	list, err := collectThreads(rows, err)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// DeletedThreadForumID 已软删主题的版块 id（回收站操作鉴权）。
func (s *Store) DeletedThreadForumID(ctx context.Context, tid int64) (int64, error) {
	var fid int64
	err := s.pool.QueryRow(ctx, `SELECT forum_id FROM threads WHERE id=$1 AND deleted`, tid).Scan(&fid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return fid, err
}

// RestoreThread 从回收站恢复主题：只恢复随主题删除的楼层（thread_deleted），
// 此前被单独删除的楼层保持隐藏，并回补版块计数。
func (s *Store) RestoreThread(ctx context.Context, tid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var forumID int64
	err = tx.QueryRow(ctx, `SELECT forum_id FROM threads WHERE id=$1 AND deleted FOR UPDATE`, tid).Scan(&forumID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// 待恢复楼层与现存公开楼层同号的先挪到当前最大楼层号之后
	// （删除期间楼层号可能被新回复占用；配合 posts_live_floor_uk 唯一索引）
	rows, err := tx.Query(ctx, `
		SELECT p.id FROM posts p
		WHERE p.thread_id=$1 AND p.deleted AND p.thread_deleted
		  AND EXISTS (SELECT 1 FROM posts q WHERE q.thread_id=$1 AND q.floor=p.floor AND NOT q.deleted)`, tid)
	if err != nil {
		return err
	}
	var conflicts []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		conflicts = append(conflicts, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range conflicts {
		if _, err := tx.Exec(ctx, `
			UPDATE posts SET floor=(SELECT coalesce(max(floor),0)+1 FROM posts WHERE thread_id=$1)
			WHERE id=$2 AND deleted AND thread_deleted`, tid, id); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE threads SET deleted=false WHERE id=$1`, tid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE posts SET deleted=false WHERE thread_id=$1 AND deleted AND thread_deleted`, tid); err != nil {
		return err
	}
	// post_count 口径 = 全部未删楼层（含待审）：删除期间的新回复与恢复楼层取并集
	if _, err := tx.Exec(ctx, `
		UPDATE threads SET post_count=(SELECT count(*) FROM posts WHERE thread_id=$1 AND NOT deleted)
		WHERE id=$1`, tid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO search_index_events(post_id) SELECT id FROM posts WHERE thread_id=$1 AND NOT deleted
ON CONFLICT(post_id) DO UPDATE SET next_attempt_at=now()`, tid); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// 作者计数回补：只补非待审楼层（待审楼层从未计入作者计数）
	if _, err := s.pool.Exec(ctx, `
		UPDATE users u SET post_count = u.post_count + x.n
		FROM (SELECT author_id, count(*) AS n FROM posts WHERE thread_id=$1 AND NOT deleted AND NOT pending GROUP BY author_id) x
		WHERE u.id = x.author_id`, tid); err != nil {
		return err
	}
	if err := s.RecomputeThreadLastPost(ctx, tid); err != nil {
		return err
	}
	return s.RecomputeForumStats(ctx, forumID)
}

// PurgeThread 彻底删除回收站中的主题（计数已在软删时扣减）。
// 事务内锁定并确认主题仍处于软删状态：与恢复并发时二者只成一个，
// 避免恢复后的主题被无条件清空楼层。
func (s *Store) PurgeThread(ctx context.Context, tid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT deleted FROM threads WHERE id=$1 FOR UPDATE`, tid).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNotFound // 已被并发恢复：拒绝删除
	}
	if _, err := tx.Exec(ctx, `DELETE FROM posts WHERE thread_id=$1`, tid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE id=$1`, tid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeRecycle 清空回收站，返回清理的主题数；forumIDs 非空时只清管辖范围内。
func (s *Store) PurgeRecycle(ctx context.Context, forumIDs []int64) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var n int64
	if len(forumIDs) > 0 {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted AND forum_id = ANY($1)`, forumIDs).Scan(&n); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM posts WHERE thread_id IN (SELECT id FROM threads WHERE deleted AND forum_id = ANY($1))`, forumIDs); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE deleted AND forum_id = ANY($1)`, forumIDs); err != nil {
			return 0, err
		}
	} else {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted`).Scan(&n); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM posts WHERE thread_id IN (SELECT id FROM threads WHERE deleted)`); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE deleted`); err != nil {
			return 0, err
		}
	}
	return n, tx.Commit(ctx)
}

// ---- 公告 ----

// Announcement 公告行。
type Announcement struct {
	ID        int64
	UID       int64
	Author    string
	Content   string
	Enabled   bool
	CreatedAt time.Time
}

// SaveAnnouncement 新增公告。
func (s *Store) SaveAnnouncement(ctx context.Context, uid int64, author, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("公告内容不能为空")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO announcements (uid, author, content) VALUES ($1,$2,$3)`, uid, author, content)
	return err
}

// SetAnnouncementEnabled 启用/停用公告。
func (s *Store) SetAnnouncementEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE announcements SET enabled=$2 WHERE id=$1`, id, enabled)
	return err
}

// DeleteAnnouncement 删除公告。
func (s *Store) DeleteAnnouncement(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM announcements WHERE id=$1`, id)
	return err
}

// Announcements 公告列表（enabledOnly=true 时仅启用的，前台用）。
func (s *Store) Announcements(ctx context.Context, enabledOnly bool, limit int) ([]*Announcement, error) {
	w := ""
	if enabledOnly {
		w = ` WHERE enabled`
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, uid, author, content, enabled, created_at FROM announcements`+w+
			` ORDER BY id DESC LIMIT `+strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Announcement
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.UID, &a.Author, &a.Content, &a.Enabled, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// ---- 敏感词 ----

// CensorWord 敏感词条目。
type CensorWord struct {
	ID          int64
	Word        string
	Replacement string
}

var (
	censorMu     sync.RWMutex
	censorCache  []CensorWord
	censorExpiry time.Time
)

// CensorWords 读取敏感词表（30s 缓存）。
func (s *Store) CensorWords(ctx context.Context) []CensorWord {
	censorMu.RLock()
	if censorCache != nil && time.Now().Before(censorExpiry) {
		v := censorCache
		censorMu.RUnlock()
		return v
	}
	censorMu.RUnlock()

	rows, err := s.pool.Query(ctx, `SELECT id, word, replacement FROM censor_words ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []CensorWord
	for rows.Next() {
		var w CensorWord
		if rows.Scan(&w.ID, &w.Word, &w.Replacement) == nil {
			out = append(out, w)
		}
	}
	censorMu.Lock()
	censorCache = out
	censorExpiry = time.Now().Add(30 * time.Second)
	censorMu.Unlock()
	return out
}

// AddCensorWord 新增敏感词。
func (s *Store) AddCensorWord(ctx context.Context, word, replacement string) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return errors.New("敏感词不能为空")
	}
	if replacement == "" {
		replacement = "*"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO censor_words (word, replacement) VALUES ($1,$2)
		 ON CONFLICT (word) DO UPDATE SET replacement = EXCLUDED.replacement`, word, replacement)
	s.invalidateCensor()
	return err
}

// DeleteCensorWord 删除敏感词。
func (s *Store) DeleteCensorWord(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM censor_words WHERE id=$1`, id)
	s.invalidateCensor()
	return err
}

func (s *Store) invalidateCensor() {
	censorMu.Lock()
	censorCache = nil
	censorMu.Unlock()
}

// ApplyCensor 对文本做敏感词替换（发帖路径调用；无词表时零开销）。
func (s *Store) ApplyCensor(ctx context.Context, text string) string {
	words := s.CensorWords(ctx)
	if len(words) == 0 {
		return text
	}
	for _, w := range words {
		text = strings.ReplaceAll(text, w.Word, w.Replacement)
	}
	return text
}
