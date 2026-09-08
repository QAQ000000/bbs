package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestHomeStatisticsCacheScopeAndExpiry(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	th, post, err := testStore.CreateThread(ctx, fid, uid, "author", "private title", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	s := New(testPool)
	var wg sync.WaitGroup
	before := testPool.Stat().AcquireCount()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.cachedHomeCounts(ctx)
			if err != nil || v.forums[fid].TotalPosts != 1 {
				t.Error("cache snapshot", err)
			}
		}()
	}
	wg.Wait()
	if n := testPool.Stat().AcquireCount() - before; n != 1 {
		t.Fatal("concurrent aggregate queries", n)
	}
	scoped := WithVisibleForums(ctx, []int64{fid})
	_, got, err := s.HomeCategoriesAndStats(scoped)
	want, exactErr := s.SiteStats(scoped)
	if err != nil || exactErr != nil || got != want {
		t.Fatal("initial statistics differ", got, want, err, exactErr)
	}
	cats, hidden, err := s.HomeCategoriesAndStats(WithVisibleForums(ctx, nil))
	if err != nil || hidden.TotalPosts != 0 || hidden.TotalThreads != 0 {
		t.Fatal("cached counts bypassed scope", hidden, err)
	}
	for _, cat := range cats {
		if len(cat.Forums) != 0 {
			t.Fatal("cached forum bypassed scope")
		}
	}
	if err = s.SetPostPendingModeration(ctx, post.ID, "review"); err != nil {
		t.Fatal(err)
	}
	cats, _, err = s.HomeCategoriesAndStats(scoped)
	if err != nil {
		t.Fatal(err)
	}
	for _, cat := range cats {
		for _, f := range cat.Forums {
			if f.LastThreadID == th.ID || f.LastThreadTitle == th.Title {
				t.Fatal("cached hidden content metadata")
			}
		}
	}
	// Fast-forward the cache deadline; the public API still uses a fixed five seconds.
	s.homeStats.mu.Lock()
	s.homeStats.counts.expires = time.Now().Add(-time.Second)
	s.homeStats.mu.Unlock()
	_, got, err = s.HomeCategoriesAndStats(scoped)
	if err != nil || got.TotalPosts != 0 || got.TodayPosts != 0 {
		t.Fatal("expired statistics not refreshed", got, err)
	}
}

func TestHomeStatisticsCanceledRefresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := New(testPool)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `LOCK TABLE posts IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	leader, cancelLeader := context.WithCancel(ctx)
	defer cancelLeader()
	done := make(chan error, 1)
	go func() { _, e := s.cachedHomeCounts(leader); done <- e }()
	for {
		s.homeStats.mu.Lock()
		started := s.homeStats.flight != nil
		s.homeStats.mu.Unlock()
		if started {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("refresh did not start")
		}
		time.Sleep(time.Millisecond)
	}
	follower, cancelFollower := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancelFollower()
	if _, err = s.cachedHomeCounts(follower); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting request was not canceled", err)
	}
	healthyDone := make(chan error, 1)
	go func() { _, e := s.cachedHomeCounts(ctx); healthyDone <- e }()
	cancelLeader()
	if err = <-done; err == nil {
		t.Fatal("canceled refresh succeeded")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-healthyDone; err != nil {
		t.Fatal("another client's cancellation poisoned refresh", err)
	}
}
