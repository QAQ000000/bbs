// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func testTitle(t *testing.T, actor int64, conditions ...TitleCondition) TitleDefinition {
	t.Helper()
	c := TitleDefinition{Name: t.Name(), Description: "task achievement", Badge: LevelBadge{Label: "Title", Icon: "star", Color: "#112233", Background: "#ddeeff"}, Status: "draft", Mode: "automatic", Match: "all", Conditions: conditions}
	// Test names are intentionally longer than user-facing labels.
	c.Name = "Task title"
	v, err := testStore.SaveTitle(context.Background(), c, actor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := testPool.Exec(context.Background(), `UPDATE titles SET body=jsonb_set(body,'{status}','"disabled"') WHERE id=$1`, v.ID); e != nil {
			t.Error(e)
		}
	})
	v.Status = "active"
	v, err = testStore.SaveTitle(context.Background(), v, actor)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func drainTitles(t *testing.T) {
	t.Helper()
	for i := 0; i < 200; i++ {
		n, err := testStore.ProcessTitleWork(context.Background(), 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			var pending int
			if err = testPool.QueryRow(context.Background(), `SELECT count(*) FROM title_jobs WHERE status='pending'`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if pending == 0 {
				return
			}
		}
	}
	t.Fatal("title work did not drain")
}

func findUserTitle(t *testing.T, uid, tid int64) UserTitle {
	t.Helper()
	rows, err := testStore.UserTitles(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range rows {
		if v.Title.ID == tid {
			return v
		}
	}
	t.Fatal("title missing")
	return UserTitle{}
}

func TestTitlesBusinessTasksAndHistoricalBackfill(t *testing.T) {
	ctx := context.Background()
	owner, reply := setupUsers(t)
	fid := setupForum(t)
	th, first, err := testStore.CreateThread(ctx, fid, owner, "owner", "title task", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := testStore.CreateReply(ctx, th.ID, reply, "reply", "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.LikeToggle(ctx, p.ID, owner); err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, true); err != nil {
		t.Fatal(err)
	}
	digest := true
	if err = testStore.SetThreadProperties(ctx, th.ID, -1, &digest, nil); err != nil {
		t.Fatal(err)
	}
	c := testTitle(t, owner, TitleCondition{Metric: "replies_created", Target: 1, ForumID: fid}, TitleCondition{Metric: "likes_received", Target: 1}, TitleCondition{Metric: "accepted_replies", Target: 1})
	writer := testTitle(t, owner, TitleCondition{Metric: "threads_created", Target: 1}, TitleCondition{Metric: "featured_threads", Target: 1})
	preview, err := testStore.PreviewTitle(ctx, c)
	if err != nil || preview.NewAwards != 1 {
		t.Fatal("historical preview", preview, err)
	}
	drainTitles(t)
	if u := findUserTitle(t, reply, c.ID); u.Status != "earned" || u.Counts[2] != 1 {
		t.Fatal(u)
	}
	if u := findUserTitle(t, owner, writer.ID); u.Status != "earned" {
		t.Fatal(u)
	}
	if m := memberXP(t, reply); m.Experience != 0 {
		t.Fatal("titles depend on XP")
	}
	if err = testStore.EquipTitle(ctx, reply, c.ID); err != nil {
		t.Fatal(err)
	}
	if err = testStore.EquipTitle(ctx, owner, c.ID); !errors.Is(err, ErrTitleForbidden) {
		t.Fatal("unearned title equipped", err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.LikeToggle(ctx, p.ID, owner); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	u := findUserTitle(t, reply, c.ID)
	if u.Status != "earned" || u.Counts[1] != 0 || u.Counts[2] != 0 {
		t.Fatal("revoked actions do not update progress", u)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, true); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	var n int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM title_logs WHERE title_id=$1 AND user_id=$2 AND action='grant'`, c.ID, reply).Scan(&n); err != nil || n != 1 {
		t.Fatal("duplicate grant", n, err)
	}
	if _, _, err = testStore.DeletePost(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u = findUserTitle(t, reply, c.ID); u.Counts[0] != 0 || u.Counts[2] != 0 {
		t.Fatal("hidden thread contributes", u)
	}
}

func TestTitlesAnyScopeModerationAndSinglePostLikes(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	fid := setupForum(t)
	excluded := setupForum(t)
	c := testTitle(t, owner, TitleCondition{Metric: "threads_created", Target: 2, ForumID: fid}, TitleCondition{Metric: "post_likes_max", Target: 1, ForumID: fid})
	c.Match = "any"
	var err error
	c, err = testStore.SaveTitle(ctx, c, owner)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = testStore.CreateThread(ctx, excluded, owner, "owner", "outside", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	th, p, err := testStore.CreateThread(ctx, fid, owner, "owner", "pending", "body", "", true, "review")
	if err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, owner, c.ID); u.Status == "earned" || u.Counts[0] != 0 {
		t.Fatal("pending/outside counted", u)
	}
	if err = testStore.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	// Even a malformed self-like inserted outside the API must not qualify.
	if _, err = testPool.Exec(ctx, `INSERT INTO post_actions(pid,uid,action) VALUES($1,$2,1)`, p.ID, owner); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, owner, c.ID); u.Status == "earned" || u.Counts[1] != 0 {
		t.Fatal("self-like counted", u)
	}
	if _, _, err = testStore.LikeToggle(ctx, p.ID, other); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, owner, c.ID); u.Status != "earned" || u.Counts[1] != 1 {
		t.Fatal("any rule failed", u)
	}
}

func TestTitlesExpiryRevocationIdempotenceAndDisable(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	c := testTitle(t, owner, TitleCondition{Metric: "experience", Target: 1})
	c.DurationDays = 1
	var err error
	c, err = testStore.SaveTitle(ctx, c, owner)
	if err != nil {
		t.Fatal(err)
	}
	a := TitleAdjustment{Action: "grant", Reason: "contribution", Key: "grant-title-once", Version: c.Version}
	if err = testStore.AdjustTitle(ctx, other, c.ID, owner, a); err != nil {
		t.Fatal(err)
	}
	before := findUserTitle(t, other, c.ID)
	if err = testStore.AdjustTitle(ctx, other, c.ID, owner, a); err != nil {
		t.Fatal(err)
	}
	a.Reason = "different"
	if err = testStore.AdjustTitle(ctx, other, c.ID, owner, a); !errors.Is(err, ErrTitleConflict) {
		t.Fatal(err)
	}
	if before.ExpiresAt == nil || before.ExpiresAt.Sub(*before.EarnedAt) < 23*time.Hour {
		t.Fatal(before)
	}
	if err = testStore.EquipTitle(ctx, other, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE user_titles SET expires_at=now()-interval '1 second' WHERE user_id=$1 AND title_id=$2`, other, c.ID); err != nil {
		t.Fatal(err)
	}
	summaries, err := testStore.EquippedTitles(ctx, []int64{other})
	if err != nil || summaries[other] != nil {
		t.Fatal("expired display", err)
	}
	if err = testStore.EquipTitle(ctx, other, c.ID); !errors.Is(err, ErrTitleForbidden) {
		t.Fatal("expired equip", err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE member_states SET experience=20 WHERE user_id=$1`, other); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, other, c.ID); u.Status != "expired" {
		t.Fatal("automatic renewed expired title", u)
	}
	a = TitleAdjustment{Action: "revoke", Reason: "invalid contribution", Key: "revoke-title-once", Version: c.Version}
	if err = testStore.AdjustTitle(ctx, other, c.ID, owner, a); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE member_states SET experience=30 WHERE user_id=$1`, other); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, other, c.ID); u.Status != "revoked" {
		t.Fatal("automatic regrant", u)
	}
	a = TitleAdjustment{Action: "grant", Reason: "appeal", Key: "grant-after-appeal", Version: c.Version}
	if err = testStore.AdjustTitle(ctx, other, c.ID, owner, a); err != nil {
		t.Fatal(err)
	}
	if err = testStore.EquipTitle(ctx, other, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Status = "paused"
	c, err = testStore.SaveTitle(ctx, c, owner)
	if err != nil {
		t.Fatal(err)
	}
	summaries, err = testStore.EquippedTitles(ctx, []int64{other})
	if err != nil || summaries[other] == nil {
		t.Fatal("paused hid existing title", err)
	}
	c.Status = "disabled"
	c, err = testStore.SaveTitle(ctx, c, owner)
	if err != nil {
		t.Fatal(err)
	}
	summaries, err = testStore.EquippedTitles(ctx, []int64{other})
	if err != nil || summaries[other] != nil {
		t.Fatal("disabled display", err)
	}
	if _, err = testStore.SaveTitle(ctx, c, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.SaveTitle(ctx, c, owner); !errors.Is(err, ErrTitleConflict) {
		t.Fatal("stale config accepted", err)
	}
}

func TestTitlesQueueAtomicityConcurrencyAndTimeRecheck(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	c := testTitle(t, owner, TitleCondition{Metric: "registered_days", Target: 1})
	drainTitles(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET days_visited=50 WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM title_events WHERE user_id=$1`, other).Scan(&n); err != nil || n != 0 {
		t.Fatal("rolled-back event survived", n, err)
	}
	// No user event occurs for the passage of time; the periodic sweep handles it.
	if _, err = testPool.Exec(ctx, `UPDATE users SET created_at=now()-interval '2 days' WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE title_schedule SET next_run=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := testStore.ProcessTitleWork(ctx, 100); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	drainTitles(t)
	if u := findUserTitle(t, other, c.ID); u.Status != "earned" {
		t.Fatal(u)
	}
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM title_logs WHERE title_id=$1 AND user_id=$2 AND action='grant'`, c.ID, other).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestAcceptanceOwnershipConcurrencyAndDeletion(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	fid := setupForum(t)
	th, first, err := testStore.CreateThread(ctx, fid, owner, "owner", "question", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := testStore.CreateReply(ctx, th.ID, other, "other", "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p2, err := testStore.CreateReply(ctx, th.ID, other, "other", "answer two", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, first.ID, owner, true); !errors.Is(err, ErrTitleForbidden) {
		t.Fatal("first accepted", err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, other, true); !errors.Is(err, ErrTitleForbidden) {
		t.Fatal("non-owner accepted", err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pid := range []int64{p.ID, p2.ID} {
		wg.Add(1)
		go func(id int64) { defer wg.Done(); results <- testStore.SetAcceptedReply(ctx, th.ID, id, owner, true) }(pid)
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrTitleConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
	pid, err := testStore.AcceptedReply(ctx, th.ID)
	if err != nil || pid == 0 {
		t.Fatal(pid, err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, pid, owner, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.DeletePost(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if pid, err = testStore.AcceptedReply(ctx, th.ID); err != nil || pid != 0 {
		t.Fatal("deleted acceptance survived", pid, err)
	}
}

func TestTitleValidationAndBlockedEligibility(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	c := testTitle(t, owner, TitleCondition{Metric: "active_days", Target: 1})
	bad := c
	bad.Conditions = []TitleCondition{{Metric: "arbitrary_sql", Target: 1}}
	if bad.Validate() == nil {
		t.Fatal("unknown metric")
	}
	bad = c
	bad.Badge.Icon = "<svg>"
	if bad.Validate() == nil {
		t.Fatal("unsafe badge")
	}
	bad = c
	bad.Conditions = []TitleCondition{{Metric: "experience", Target: 1, ForumID: 1}}
	if bad.Validate() == nil {
		t.Fatal("scoped account metric")
	}
	if _, err := testPool.Exec(ctx, `UPDATE users SET days_visited=10,banned_until=now()+interval '1 hour' WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, other, c.ID); u.Status == "earned" {
		t.Fatal("banned user awarded", u)
	}
	if _, err := testPool.Exec(ctx, `UPDATE users SET banned_until=NULL WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	drainTitles(t)
	if u := findUserTitle(t, other, c.ID); u.Status != "earned" {
		t.Fatal("unban not rechecked", u)
	}
}
