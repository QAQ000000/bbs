// SPDX-License-Identifier: AGPL-3.0-or-later
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

// RecycleThreads 回收站主题分页。
func (s *Store) RecycleThreads(ctx context.Context, page, size int) ([]*Thread, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+threadCols+` `+threadJoins+`
		 WHERE t.deleted ORDER BY t.last_post_at DESC LIMIT $`+strconv.Itoa(1)+
			` OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Thread
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.ForumID, &t.AuthorID, &t.AuthorName, &t.Title,
			&t.Sticky, &t.Digest, &t.Closed, &t.PostCount, &t.ViewCount,
			&t.CreatedAt, &t.LastPostAt, &t.LastPostUID, &t.LastPostName, &t.FirstPostID); err != nil {
			return nil, 0, err
		}
		out = append(out, &t)
	}
	return out, total, rows.Err()
}

// RestoreThread 从回收站恢复主题：连同其软删楼层一并恢复，并回补版块计数。
func (s *Store) RestoreThread(ctx context.Context, tid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var forumID int64
	var postCount int
	err = tx.QueryRow(ctx, `SELECT forum_id, post_count FROM threads WHERE id=$1 AND deleted`, tid).Scan(&forumID, &postCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	var restored int
	if err := tx.QueryRow(ctx,
		`WITH u AS (UPDATE threads SET deleted=false WHERE id=$1 RETURNING 1)
		 SELECT count(*) FROM u`, tid).Scan(&restored); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE posts SET deleted=false WHERE thread_id=$1 AND deleted`, tid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE forums SET thread_count=thread_count+$2, post_count=post_count+$3 WHERE id=$1`,
		forumID, restored, postCount); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeThread 彻底删除回收站中的主题（计数已在软删时扣减）。
func (s *Store) PurgeThread(ctx context.Context, tid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM posts WHERE thread_id=$1`, tid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE id=$1 AND deleted`, tid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeRecycle 清空回收站，返回清理的主题数。
func (s *Store) PurgeRecycle(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var n int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM threads WHERE deleted`).Scan(&n); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM posts WHERE thread_id IN (SELECT id FROM threads WHERE deleted)`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE deleted`); err != nil {
		return 0, err
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
