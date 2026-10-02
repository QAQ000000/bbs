package store

import (
	"context"
	"time"
)

type PopularThread struct {
	ID      int64  `json:"id,string"`
	Title   string `json:"title"`
	Replies int64  `json:"replies"`
	Views   int64  `json:"views"`
}

type PopularAuthor struct {
	UserID   int64  `json:"userId,string"`
	Username string `json:"username"`
	Posts    int64  `json:"posts"`
	Likes    int64  `json:"likes"`
}

type PopularView struct {
	Since       time.Time       `json:"since"`
	GeneratedAt time.Time       `json:"generatedAt"`
	Threads     []PopularThread `json:"threads"`
	Authors     []PopularAuthor `json:"authors"`
}

// Scope before aggregation: restricted and moderated content cannot affect rankings.
func (s *Store) Popular(ctx context.Context, at time.Time) (PopularView, error) {
	v := PopularView{Since: at.AddDate(0, 0, -7), GeneratedAt: at, Threads: []PopularThread{}, Authors: []PopularAuthor{}}
	rows, err := s.pool.Query(ctx, `SELECT t.id,t.title,
	 (SELECT count(*) FROM posts p WHERE p.thread_id=t.id AND p.floor>1 AND NOT p.deleted AND NOT p.pending),t.view_count
	 FROM threads t JOIN users u ON u.id=t.author_id
	 WHERE NOT t.deleted AND NOT t.pending AND t.created_at>=$1 AND t.created_at<$2
	 AND (u.blocked_until IS NULL OR u.blocked_until<=now())`+forumFilter(ctx, "t.forum_id")+`
	 ORDER BY 3 DESC,t.view_count DESC,t.id DESC LIMIT 5`, v.Since, at)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var item PopularThread
		if err = rows.Scan(&item.ID, &item.Title, &item.Replies, &item.Views); err != nil {
			rows.Close()
			return v, err
		}
		v.Threads = append(v.Threads, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	rows, err = s.pool.Query(ctx, `SELECT u.id,u.username,count(*),coalesce(sum(p.like_count),0)
	 FROM posts p JOIN threads t ON t.id=p.thread_id JOIN users u ON u.id=p.author_id
	 WHERE NOT p.deleted AND NOT p.pending AND NOT t.deleted AND NOT t.pending
	 AND p.created_at>=$1 AND p.created_at<$2 AND (u.blocked_until IS NULL OR u.blocked_until<=now())`+forumFilter(ctx, "t.forum_id")+`
	 GROUP BY u.id,u.username ORDER BY 4 DESC,3 DESC,u.id LIMIT 5`, v.Since, at)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var item PopularAuthor
		if err = rows.Scan(&item.UserID, &item.Username, &item.Posts, &item.Likes); err != nil {
			return v, err
		}
		v.Authors = append(v.Authors, item)
	}
	return v, rows.Err()
}
