// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"time"
)

func (s *Store) ReplyTargetAuthor(ctx context.Context, pid int64) (int64, error) {
	var uid int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(q.author_id,0) FROM posts p
 LEFT JOIN posts q ON q.id=p.reply_to_post_id AND q.thread_id=p.thread_id AND NOT q.deleted AND NOT q.pending
 WHERE p.id=$1`, pid).Scan(&uid)
	return uid, err
}

type ReplyTarget struct {
	ID         string `json:"id"`
	Available  bool   `json:"available"`
	Floor      int    `json:"floor,omitempty"`
	AuthorID   string `json:"authorId,omitempty"`
	AuthorName string `json:"authorName,omitempty"`
}

type PostViewerState struct {
	Liked   bool
	ReplyTo *ReplyTarget
}

// Callers authorize source posts first. Hidden reply targets expose only identity.
func (s *Store) PostViewerStates(ctx context.Context, ids []int64, uid int64) (map[int64]PostViewerState, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id, EXISTS(SELECT 1 FROM post_actions l WHERE l.pid=p.id AND l.uid=$2 AND l.action=1),
 coalesce(p.reply_to_post_id,0)::text, q.id IS NOT NULL,coalesce(q.floor,0),coalesce(q.author_id,0)::text,coalesce(u.username,'')
 FROM posts p LEFT JOIN posts q ON q.id=p.reply_to_post_id AND q.thread_id=p.thread_id AND NOT q.deleted AND NOT q.pending
 LEFT JOIN users u ON u.id=q.author_id WHERE p.id=ANY($1)`, ids, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]PostViewerState{}
	for rows.Next() {
		var id int64
		var st PostViewerState
		var target ReplyTarget
		if err := rows.Scan(&id, &st.Liked, &target.ID, &target.Available, &target.Floor, &target.AuthorID, &target.AuthorName); err != nil {
			return nil, err
		}
		if target.ID != "0" {
			if !target.Available {
				target.AuthorID = ""
			}
			st.ReplyTo = &target
		}
		out[id] = st
	}
	return out, rows.Err()
}

func (s *Store) PostPosition(ctx context.Context, p *Post, uid int64, moderator bool, size int) (int, error) {
	var preceding int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM posts WHERE thread_id=$1 AND floor<$2 AND NOT deleted
 AND (NOT pending OR author_id=$3 OR $4)`, p.ThreadID, p.Floor, uid, moderator).Scan(&preceding)
	return preceding/size + 1, err
}

func (s *Store) ThreadApprovalPostIDs(ctx context.Context, tid int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM posts WHERE thread_id=$1 AND NOT deleted
	 AND (pending OR EXISTS(SELECT 1 FROM threads WHERE id=$1 AND pending)) ORDER BY floor`, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type OwnContent struct {
	ID              string    `json:"id"`
	ThreadID        string    `json:"threadId"`
	ForumID         string    `json:"forumId"`
	Floor           int       `json:"floor"`
	Subject         string    `json:"subject"`
	Content         string    `json:"content"`
	Status          string    `json:"status"`
	ModerationNote  string    `json:"moderationNote"`
	ParentAvailable bool      `json:"parentAvailable"`
	CreatedAt       time.Time `json:"createdAt"`
}

func (s *Store) OwnContentPage(ctx context.Context, uid int64, kind, status string, page, size int) ([]OwnContent, int, error) {
	state := `CASE WHEN p.deleted AND p.moderation_status='rejected' THEN 'rejected'
 WHEN p.deleted OR t.deleted THEN 'deleted' WHEN p.pending OR t.pending THEN 'pending' ELSE 'published' END`
	filter := ` FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.author_id=$1
 AND (($2='threads' AND p.floor=1) OR ($2='replies' AND p.floor>1)) AND ($3='all' OR ` + state + `=$3)` + forumFilter(ctx, "t.forum_id")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+filter, uid, kind, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,t.id::text,t.forum_id::text,p.floor,
 CASE WHEN t.author_id=$1 OR (NOT t.pending AND NOT t.deleted) THEN t.title ELSE '' END,
 p.content_md,`+state+`,p.moderation_note,NOT t.pending AND NOT t.deleted,p.created_at`+filter+`
 ORDER BY p.id DESC LIMIT $4 OFFSET $5`, uid, kind, status, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []OwnContent{}
	for rows.Next() {
		var v OwnContent
		if err := rows.Scan(&v.ID, &v.ThreadID, &v.ForumID, &v.Floor, &v.Subject, &v.Content, &v.Status, &v.ModerationNote, &v.ParentAvailable, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
