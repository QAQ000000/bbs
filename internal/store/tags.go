// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Tag struct {
	ID          int64  `json:"id,string"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Status      string `json:"status"`
	Version     int    `json:"version"`
	ThreadCount int    `json:"threadCount"`
	// Subscribed 仅在详情接口按当前登录用户设置；nil 表示未计算。
	Subscribed *bool `json:"subscribed,omitempty"`
}

var tagSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var tagColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func communityDBError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return ErrCommunityConflict
	}
	return err
}

func lockTags(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('community-tags',0))`)
	return err
}

func (s *Store) SaveTag(ctx context.Context, t Tag, actor int64) (Tag, error) {
	t.Name = strings.ToLower(strings.Join(strings.Fields(t.Name), " "))
	t.Slug = strings.ToLower(strings.TrimSpace(t.Slug))
	if t.Name == "" || utf8.RuneCountInString(t.Name) > 32 || !tagSlug.MatchString(t.Slug) || utf8.RuneCountInString(t.Description) > 500 || (t.Color != "" && !tagColor.MatchString(t.Color)) || (t.Status != "active" && t.Status != "disabled") {
		return t, ErrCommunityInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return t, err
	}
	defer tx.Rollback(ctx)
	if err = lockTags(ctx, tx); err != nil {
		return t, err
	}
	var oldSlug string
	var oldVersion int
	if t.ID != 0 {
		if err = tx.QueryRow(ctx, `SELECT slug,version FROM tags WHERE id=$1 FOR UPDATE`, t.ID).Scan(&oldSlug, &oldVersion); err != nil {
			return t, communityDBError(err)
		}
		if oldVersion != t.Version {
			return t, ErrCommunityConflict
		}
	}
	var collision bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tag_aliases WHERE alias=$1 AND tag_id<>$2)`, t.Slug, t.ID).Scan(&collision); err != nil {
		return t, err
	}
	if collision {
		return t, ErrCommunityConflict
	}
	if t.ID == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO tags(name,slug,description,color,status,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,version`, t.Name, t.Slug, t.Description, t.Color, t.Status, actor).Scan(&t.ID, &t.Version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE tags SET name=$2,slug=$3,description=$4,color=$5,status=$6,version=version+1 WHERE id=$1 RETURNING version`, t.ID, t.Name, t.Slug, t.Description, t.Color, t.Status).Scan(&t.Version)
		if err == nil && oldSlug != t.Slug {
			_, err = tx.Exec(ctx, `INSERT INTO tag_aliases(alias,tag_id) VALUES($1,$2) ON CONFLICT(alias) DO UPDATE SET tag_id=EXCLUDED.tag_id`, oldSlug, t.ID)
		}
	}
	if err != nil {
		return t, communityDBError(err)
	}
	return t, tx.Commit(ctx)
}

const tagCols = `g.id,g.name,g.slug,g.description,g.color,g.status,g.version`

func (s *Store) Tag(ctx context.Context, id int64) (Tag, error) {
	var t Tag
	err := s.pool.QueryRow(ctx, `SELECT `+tagCols+` FROM tags g WHERE g.id=$1`, id).Scan(&t.ID, &t.Name, &t.Slug, &t.Description, &t.Color, &t.Status, &t.Version)
	return t, communityDBError(err)
}
func (s *Store) ResolveTag(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM tags WHERE slug=$1 UNION SELECT tag_id FROM tag_aliases WHERE alias=$1 LIMIT 1`, strings.ToLower(slug)).Scan(&id)
	return id, communityDBError(err)
}
func (s *Store) Tags(ctx context.Context, page int, all bool, query string) ([]Tag, int, error) {
	filter := ` WHERE ($1::bool OR g.status='active') AND ($2='' OR strpos(g.name,lower($2))>0 OR strpos(g.slug,lower($2))>0)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tags g`+filter, all, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+tagCols+`,(SELECT count(*) FROM thread_tags tt JOIN threads t ON t.id=tt.thread_id
 WHERE tt.tag_id=g.id AND NOT t.deleted AND NOT t.pending`+forumFilter(ctx, "t.forum_id")+`) FROM tags g`+filter+` ORDER BY g.name,g.id LIMIT 30 OFFSET $3`, all, query, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Description, &t.Color, &t.Status, &t.Version, &t.ThreadCount); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func setThreadTags(ctx context.Context, tx pgx.Tx, tid int64, ids []int64) error {
	if len(ids) > 8 {
		return ErrCommunityInvalid
	}
	if err := lockTags(ctx, tx); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return ErrCommunityInvalid
		}
		seen[id] = true
	}
	var valid int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE id=ANY($1::bigint[]) AND
 (status='active' OR EXISTS(SELECT 1 FROM thread_tags WHERE thread_id=$2 AND tag_id=tags.id))`, ids, tid).Scan(&valid); err != nil {
		return err
	}
	if valid != len(ids) {
		return ErrCommunityInvalid
	}
	if _, err := tx.Exec(ctx, `DELETE FROM thread_tags WHERE thread_id=$1 AND NOT (tag_id=ANY($2::bigint[]))`, tid, append([]int64{}, ids...)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO thread_tags(thread_id,tag_id) SELECT $1,unnest($2::bigint[]) ON CONFLICT DO NOTHING`, tid, ids)
	return err
}

func (s *Store) SetThreadTags(ctx context.Context, tid int64, version int, ids []int64) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var pid int64
	err = tx.QueryRow(ctx, `SELECT first_post_id FROM threads t WHERE t.id=$1 AND NOT t.deleted`+forumFilter(ctx, "t.forum_id")+` FOR UPDATE`, tid).Scan(&pid)
	if err != nil {
		return 0, communityDBError(err)
	}
	var next int
	err = tx.QueryRow(ctx, `UPDATE posts SET version=version+1 WHERE id=$1 AND version=$2 AND NOT deleted RETURNING version`, pid, version).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrCommunityConflict
	}
	if err != nil {
		return 0, err
	}
	if err = setThreadTags(ctx, tx, tid, ids); err != nil {
		return 0, err
	}
	return next, tx.Commit(ctx)
}

// Callers authorize the source threads before requesting their tag summaries.
func (s *Store) ThreadTags(ctx context.Context, ids []int64) (map[int64][]Tag, error) {
	rows, err := s.pool.Query(ctx, `SELECT tt.thread_id,`+tagCols+` FROM thread_tags tt JOIN tags g ON g.id=tt.tag_id WHERE tt.thread_id=ANY($1) ORDER BY g.name,g.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Tag{}
	for _, id := range ids {
		out[id] = []Tag{}
	}
	for rows.Next() {
		var id int64
		var t Tag
		if err := rows.Scan(&id, &t.ID, &t.Name, &t.Slug, &t.Description, &t.Color, &t.Status, &t.Version); err != nil {
			return nil, err
		}
		out[id] = append(out[id], t)
	}
	return out, rows.Err()
}

func (s *Store) TaggedThreads(ctx context.Context, tagID int64, page, size int) ([]*Thread, int, error) {
	filter := ` WHERE NOT t.deleted AND NOT t.pending AND EXISTS(SELECT 1 FROM thread_tags WHERE thread_id=t.id AND tag_id=$1)` + forumFilter(ctx, "t.forum_id")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM threads t`+filter, tagID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+threadCols+` `+threadJoins+filter+` ORDER BY t.last_post_at DESC,t.id DESC LIMIT $2 OFFSET $3`, tagID, size, (page-1)*size)
	list, err := collectThreads(rows, err)
	return list, total, err
}
