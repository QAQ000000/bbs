package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPerformanceSingleConnectionHome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fid := setupForum(t)
	cfg := testPool.Config()
	cfg.MaxConns, cfg.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := New(pool)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cats, err := s.CategoriesWithForums(WithVisibleForums(ctx, []int64{fid}))
			if err == nil && len(cats) == 0 {
				t.Error("missing categories")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("connection leaked")
	}
}

func TestPerformanceStatisticsVisibility(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, uid, "author", "today", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, yesterday, err := testStore.CreateReply(ctx, th.ID, uid, "author", "yesterday", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, old, err := testStore.CreateReply(ctx, th.ID, uid, "author", "old", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE posts SET created_at=CASE WHEN id=$1 THEN current_date-interval '1 hour' ELSE current_date-interval '3 days' END WHERE id=ANY($2)`, yesterday.ID, []int64{yesterday.ID, old.ID}); err != nil {
		t.Fatal(err)
	}
	hidden, _, err := testStore.CreateThread(ctx, fid, uid, "author", "hidden", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetPostPendingModeration(ctx, hidden.FirstPostID, "review"); err != nil {
		t.Fatal(err)
	}
	// A public child under a hidden parent must also be excluded.
	if _, err = testPool.Exec(ctx, `INSERT INTO posts(thread_id,author_id,floor,content_md,content_html) VALUES($1,$2,2,'child','')`, hidden.ID, uid); err != nil {
		t.Fatal(err)
	}
	scoped := WithVisibleForums(ctx, []int64{fid})
	stats, err := testStore.SiteStats(scoped)
	if err != nil || stats.TodayPosts != 1 || stats.Yesterday != 1 || stats.TotalPosts != 3 || stats.TotalThreads != 1 {
		t.Fatalf("statistics: %+v %v", stats, err)
	}
	forum, err := testStore.Forum(scoped, fid)
	if err != nil || forum.TodayCount != 1 {
		t.Fatalf("forum: %+v %v", forum, err)
	}
	cats, err := testStore.CategoriesWithForums(scoped)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		for _, f := range c.Forums {
			if f.ID != fid || f.TodayCount != 1 {
				t.Fatalf("leaked/inaccurate forum: %+v", f)
			}
		}
	}
	stats, err = testStore.SiteStats(WithVisibleForums(ctx, nil))
	if err != nil || stats.TodayPosts != 0 || stats.TotalPosts != 0 || stats.TotalThreads != 0 {
		t.Fatalf("empty scope: %+v %v", stats, err)
	}
}

func TestPerformanceThreadFeedTiesAndVisibility(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	ids := []int64{}
	for i := 0; i < 7; i++ {
		th, _, err := testStore.CreateThread(ctx, fid, uid, "author", "feed", "body", "", false, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	if _, err := testPool.Exec(ctx, `UPDATE threads SET last_post_at='2026-01-01',pending=id=$2,deleted=id=$3 WHERE id=ANY($1)`, ids, ids[5], ids[6]); err != nil {
		t.Fatal(err)
	}
	scoped := WithVisibleForums(ctx, []int64{fid})
	for _, target := range []int64{0, fid} {
		var after *ThreadCursor
		seen := map[int64]bool{}
		for page := 0; ; page++ {
			if page > 4 {
				t.Fatal("feed did not terminate")
			}
			rows, more, err := testStore.ThreadFeed(scoped, target, 2, after)
			if err != nil {
				t.Fatal(err)
			}
			for _, th := range rows {
				if seen[th.ID] || th.ID == ids[5] || th.ID == ids[6] {
					t.Fatal("duplicate/hidden thread", th.ID)
				}
				seen[th.ID] = true
			}
			if !more {
				break
			}
			last := rows[len(rows)-1]
			after = &ThreadCursor{ForumID: target, ID: last.ID, LastPostAt: last.LastPostAt}
		}
		if len(seen) != 5 {
			t.Fatal("missing rows", seen)
		}
	}
	rows, more, err := testStore.ThreadFeed(WithVisibleForums(ctx, nil), 0, 2, nil)
	if err != nil || len(rows) != 0 || more {
		t.Fatal("scope bypass", rows, more, err)
	}
}

func TestPerformanceMembershipSnapshot(t *testing.T) {
	ctx := context.Background()
	c, err := testStore.MembershipConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testPool.Config()
	cfg.MaxConns, cfg.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := New(pool)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	deadline, cancel := context.WithTimeout(WithMembershipSnapshot(ctx, c), 100*time.Millisecond)
	defer cancel()
	got, err := s.MembershipConfig(deadline)
	if err != nil || got.Version != c.Version {
		t.Fatal("snapshot tried to acquire a connection", err)
	}
	if _, err = s.FreshMembershipConfig(deadline); err == nil {
		t.Fatal("fresh config reused stale snapshot")
	}
	conn.Release()
	if _, err = s.FreshMembershipConfig(WithMembershipSnapshot(ctx, c)); err != nil {
		t.Fatal(err)
	}
}
