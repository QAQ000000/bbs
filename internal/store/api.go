// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import "context"

// VisiblePosts paginates after applying per-viewer moderation visibility. A
// thread author cannot see another author's pending reply merely by owning it.
func (s *Store) VisiblePosts(ctx context.Context, tid, viewerID int64, moderator bool, page, size int) ([]*Post, int, error) {
	const filter = ` WHERE p.thread_id=$1 AND NOT p.deleted AND (NOT p.pending OR p.author_id=$2 OR $3)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM posts p`+filter, tid, viewerID, moderator).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+postCols+` `+postJoins+filter+` ORDER BY p.floor LIMIT $4 OFFSET $5`, tid, viewerID, moderator, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*Post, 0)
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.AuthorID, &p.AuthorName, &p.AuthorGroup, &p.Floor, &p.ContentMD, &p.ContentHTML, &p.CreatedAt, &p.EditedAt, &p.HasEdited, &p.Pending, &p.PendingReason, &p.LikeCount, &p.Version, &p.IP); err != nil {
			return nil, 0, err
		}
		out = append(out, &p)
	}
	return out, total, rows.Err()
}
