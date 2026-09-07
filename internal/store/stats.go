package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type statsDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func lockForumStats(ctx context.Context, tx pgx.Tx, forumID int64) error {
	var id int64
	return tx.QueryRow(ctx, `SELECT id FROM forums WHERE id=$1 FOR NO KEY UPDATE`, forumID).Scan(&id)
}

// ---- 版块统计口径 ----
//
// 所有版块级统计（thread_count/post_count/last_*）一律为“公开口径”：
// 不含待审核（pending）与已删除（deleted）内容。待审核内容在审批通过时
// 通过 RecomputeForumStats 并入；today_count 为读取时实时计算，永不漂移。

// RecomputeForumStats 从公开内容重算单个版块的全部统计与最后发表。
func (s *Store) RecomputeForumStats(ctx context.Context, forumID int64) error {
	return recomputeForumStats(ctx, s.pool, forumID)
}

func recomputeForumStats(ctx context.Context, db statsDB, forumID int64) error {
	// A separate lock statement gives the aggregate a fresh snapshot after any wait.
	if tx, ok := db.(pgx.Tx); ok {
		if err := lockForumStats(ctx, tx, forumID); err != nil {
			return err
		}
	}
	// 标量子查询可引用 UPDATE 目标行（UPDATE...FROM 里 LATERAL 不能引用目标表）。
	// 主题与楼层状态同时约束：待审/已删主题下的公开回复同样不进公开口径，
	// 否则首楼被重新送审后，last_* 仍会引用隐藏主题的标题与作者。
	latest := func(col string) string {
		return `(SELECT ` + col + ` FROM posts p
			JOIN threads t ON t.id = p.thread_id
			JOIN users u ON u.id = p.author_id
			WHERE t.forum_id = f.id AND NOT p.deleted AND NOT p.pending
			  AND NOT t.deleted AND NOT t.pending
			ORDER BY p.created_at DESC
			LIMIT 1)`
	}
	_, err := db.Exec(ctx, `
		UPDATE forums f SET
			thread_count = (SELECT count(*) FROM threads t
				WHERE t.forum_id=f.id AND NOT t.deleted AND NOT t.pending),
			post_count = (SELECT count(*) FROM posts p JOIN threads t ON t.id=p.thread_id
				WHERE t.forum_id=f.id AND NOT p.deleted AND NOT p.pending
				  AND NOT t.deleted AND NOT t.pending),
			last_post_at = `+latest("p.created_at")+`,
			last_post_uid = `+latest("p.author_id")+`,
			last_post_author = `+latest("u.username")+`,
			last_thread_id = `+latest("t.id")+`,
			last_thread_title = `+latest("t.title")+`
		WHERE f.id = $1`, forumID)
	return err
}

// RecomputeAllForumStats 启动时对全部版块执行一次口径重算
// （修复历史漂移：删除未回补、测试残留等）。
func (s *Store) RecomputeAllForumStats(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id FROM forums`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.RecomputeForumStats(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// RecomputeThreadLastPost 主题级最后发表回填（审批通过后调用）。
func (s *Store) RecomputeThreadLastPost(ctx context.Context, threadID int64) error {
	return recomputeThreadLastPost(ctx, s.pool, threadID)
}

func recomputeThreadLastPost(ctx context.Context, db statsDB, threadID int64) error {
	_, err := db.Exec(ctx, `
		UPDATE threads t SET
			last_post_at = (SELECT p.created_at FROM posts p
				WHERE p.thread_id = t.id AND NOT p.deleted AND NOT p.pending
				ORDER BY p.created_at DESC LIMIT 1),
			last_post_uid = (SELECT p.author_id FROM posts p
				WHERE p.thread_id = t.id AND NOT p.deleted AND NOT p.pending
				ORDER BY p.created_at DESC LIMIT 1)
		WHERE t.id = $1`, threadID)
	return err
}

// ApproveThreadPendingAuthors 审批前读取待审核楼层的作者分布（用于回补发帖计数）。
func (s *Store) ApproveThreadPendingAuthors(ctx context.Context, threadID int64) ([]struct {
	UID   int64
	Count int
}, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT author_id, count(*) FROM posts WHERE thread_id=$1 AND pending AND NOT deleted GROUP BY author_id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		UID   int64
		Count int
	}
	for rows.Next() {
		var uid int64
		var n int
		if err := rows.Scan(&uid, &n); err != nil {
			return nil, err
		}
		out = append(out, struct {
			UID   int64
			Count int
		}{uid, n})
	}
	return out, rows.Err()
}

// BumpUsersPostCount 按差值调整用户发帖计数（不低于 0）。
func (s *Store) BumpUsersPostCount(ctx context.Context, uid int64, delta int64) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		_, err := s.pool.Exec(ctx, `UPDATE users SET post_count = post_count + $2 WHERE id=$1`, uid, delta)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET post_count = GREATEST(post_count + $2, 0) WHERE id=$1`, uid, delta)
	return err
}
