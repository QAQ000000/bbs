package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const homeStatsTTL = 5 * time.Second

type homeCounts struct {
	forums  map[int64]SiteStats
	members int64
	expires time.Time
}

type homeStatsFlight struct {
	done   chan struct{}
	counts *homeCounts
	err    error
}

type homeStatsCache struct {
	mu     sync.Mutex
	counts *homeCounts
	flight *homeStatsFlight
}

// Only anonymous numerical aggregates are shared. Metadata and access scope are
// read afresh, so a cached private forum cannot reappear after access is revoked.
func (s *Store) HomeCategoriesAndStats(ctx context.Context) ([]*Category, SiteStats, error) {
	counts, err := s.cachedHomeCounts(ctx)
	if err != nil {
		return nil, SiteStats{}, err
	}
	cats, err := s.categoriesWithForums(ctx, counts)
	if err != nil {
		return nil, SiteStats{}, err
	}
	stats := SiteStats{Members: counts.members}
	for _, cat := range cats {
		for _, forum := range cat.Forums {
			v := counts.forums[forum.ID]
			stats.TodayPosts += v.TodayPosts
			stats.Yesterday += v.Yesterday
			stats.TotalPosts += v.TotalPosts
			stats.TotalThreads += v.TotalThreads
		}
	}
	return cats, stats, nil
}

func (s *Store) cachedHomeCounts(ctx context.Context) (*homeCounts, error) {
	c := &s.homeStats
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.counts != nil && time.Now().Before(c.counts.expires) {
			v := c.counts
			c.mu.Unlock()
			return v, nil
		}
		if f := c.flight; f != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.done:
				// Another client's cancellation must not cancel this request's refresh.
				if errors.Is(f.err, context.Canceled) || errors.Is(f.err, context.DeadlineExceeded) {
					continue
				}
				return f.counts, f.err
			}
		}
		f := &homeStatsFlight{done: make(chan struct{})}
		c.flight = f
		c.mu.Unlock()
		v, err := s.loadHomeCounts(ctx)
		c.mu.Lock()
		if err == nil {
			c.counts = v
		}
		f.counts, f.err = v, err
		c.flight = nil
		close(f.done)
		c.mu.Unlock()
		return v, err
	}
}

func (s *Store) loadHomeCounts(ctx context.Context) (*homeCounts, error) {
	v := &homeCounts{forums: map[int64]SiteStats{}, expires: time.Now().Add(homeStatsTTL)}
	var body []byte
	var midnight time.Time
	err := s.pool.QueryRow(ctx, `WITH public_posts AS (
	 SELECT t.forum_id,count(*) AS total,
	 count(*) FILTER (WHERE p.created_at>=current_date) AS today,
	 count(*) FILTER (WHERE p.created_at>=current_date-1 AND p.created_at<current_date) AS yesterday
	 FROM posts p JOIN threads t ON t.id=p.thread_id
	 WHERE NOT p.deleted AND NOT p.pending AND NOT t.deleted AND NOT t.pending GROUP BY t.forum_id
	), public_threads AS (
	 SELECT forum_id,count(*) AS total FROM threads WHERE NOT deleted AND NOT pending GROUP BY forum_id
	) SELECT coalesce(jsonb_agg(jsonb_build_object('ForumID',f.id,
	 'TodayPosts',coalesce(p.today,0),'Yesterday',coalesce(p.yesterday,0),
	 'TotalPosts',coalesce(p.total,0),'TotalThreads',coalesce(t.total,0))),'[]'),
	 (SELECT count(*) FROM users),(current_date+1)::timestamptz
	 FROM forums f LEFT JOIN public_posts p ON p.forum_id=f.id LEFT JOIN public_threads t ON t.forum_id=f.id`).Scan(&body, &v.members, &midnight)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ForumID int64
		SiteStats
	}
	if err = json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		v.forums[r.ForumID] = r.SiteStats
	}
	if midnight.Before(v.expires) {
		v.expires = midnight
	}
	return v, nil
}
