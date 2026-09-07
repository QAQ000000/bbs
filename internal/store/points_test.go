package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func pointsTestConfig(t *testing.T) PointsConfig {
	t.Helper()
	c, err := testStore.PointsConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `UPDATE points_config SET version=$1,body=$2 WHERE id`, c.Version, raw); err != nil {
			t.Error(err)
		}
	})
	return c
}
func pointsBalance(t *testing.T, uid int64, want int64) PointsAccount {
	t.Helper()
	a, err := testStore.PointsAccount(context.Background(), uid)
	if err != nil || a.Balance != want {
		t.Fatal(a, err, "want", want)
	}
	report, err := testStore.ReconcilePoints(context.Background(), uid)
	if err != nil || report["consistent"] != true {
		t.Fatal(report, err)
	}
	return a
}

func TestPointsDefaultsSnapshotCapAndReversal(t *testing.T) {
	ctx := context.Background()
	c := pointsTestConfig(t)
	uid, actor := setupUsers(t)
	fid := setupForum(t)
	raw, _ := json.Marshal(c)
	def, _ := json.Marshal(DefaultPointsConfig())
	if string(raw) != string(def) {
		t.Fatal("points SQL defaults differ")
	}
	c.Rules["thread"] = GrowthRule{true, 2, 3, true}
	if err := testStore.SavePointsConfig(ctx, c, actor); err != nil {
		t.Fatal(err)
	}
	c.Version++
	_, p1, err := testStore.CreateThread(ctx, fid, uid, "author", "points one", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = testStore.CreateThread(ctx, fid, uid, "author", "points two", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	// Event payloads retain the previous version, even before any worker has run.
	c.Rules["thread"] = GrowthRule{true, 100, 1000, false}
	if err = testStore.SavePointsConfig(ctx, c, actor); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, uid, 3)
	if _, _, err = testStore.DeletePost(ctx, p1.ID); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, uid, 1)
	if _, err = testPool.Exec(ctx, `SELECT member_enqueue($1,'thread',$2,true)`, uid, fmt.Sprint("post:", p1.ID)); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, uid, 1)
	if _, err = testPool.Exec(ctx, `UPDATE points_ledger SET delta=999 WHERE user_id=$1`, uid); err == nil {
		t.Fatal("ledger mutation allowed")
	}
	if _, err = testPool.Exec(ctx, `DELETE FROM points_ledger WHERE user_id=$1`, uid); err == nil {
		t.Fatal("ledger removal allowed")
	}
}

func TestPointsDebtAndAdminIdempotency(t *testing.T) {
	ctx := context.Background()
	uid, actor := setupUsers(t)
	fid := setupForum(t)
	_, p, err := testStore.CreateThread(ctx, fid, uid, "author", "points debt", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	a := pointsBalance(t, uid, 1)
	v := PointsAdjustment{Version: a.Version, Delta: -1, Reason: "test deduction", Key: "points-debit-001"}
	if err = testStore.AdjustPoints(ctx, uid, actor, v); err != nil {
		t.Fatal(err)
	}
	if err = testStore.AdjustPoints(ctx, uid, actor, v); err != nil {
		t.Fatal("idempotent replay failed", err)
	}
	v.Reason = "changed payload"
	if err = testStore.AdjustPoints(ctx, uid, actor, v); !errors.Is(err, ErrPointsConflict) {
		t.Fatal(err)
	}
	if _, _, err = testStore.DeletePost(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	a = pointsBalance(t, uid, -1)
	if a.Available != 0 || a.Debt != 1 {
		t.Fatal(a)
	}
	if err = testStore.AdjustPoints(ctx, uid, actor, PointsAdjustment{Version: a.Version, Delta: -1, Reason: "no overdraft", Key: "points-debit-002"}); !errors.Is(err, ErrPointsInsufficient) {
		t.Fatal(err)
	}
	if err = testStore.AdjustPoints(ctx, uid, actor, PointsAdjustment{Version: a.Version, Delta: 2, Reason: "compensation", Key: "points-credit-001"}); err != nil {
		t.Fatal(err)
	}
	a = pointsBalance(t, uid, 1)
	if a.Debt != 0 || a.Available != 1 {
		t.Fatal(a)
	}
	var count int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM admin_logs WHERE uid=$1 AND action='points.adjust'`, actor).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
}

func TestPointsConcurrentAdjustmentAndRollback(t *testing.T) {
	ctx := context.Background()
	uid, actor := setupUsers(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- testStore.AdjustPoints(ctx, uid, actor, PointsAdjustment{Version: 1, Delta: 5, Reason: "concurrent", Key: fmt.Sprintf("concurrent-%d", i)})
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrPointsConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	a := pointsBalance(t, uid, 5)
	// Nonexistent audit actor forces failure after both account and ledger writes.
	if err := testStore.AdjustPoints(ctx, uid, 99999999, PointsAdjustment{Version: a.Version, Delta: 8, Reason: "rollback", Key: "rollback-001"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	pointsBalance(t, uid, 5)
}

func TestPointsAcceptedExperienceAndNoRepeatAward(t *testing.T) {
	ctx := context.Background()
	owner, author := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, owner, "owner", "accepted points", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := testStore.CreateReply(ctx, th.ID, author, "answerer", "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	xp := memberXP(t, author).Experience
	pointsBalance(t, author, 1)
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, true); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, author, 11)
	if memberXP(t, author).Experience != xp+30 {
		t.Fatal("accepted experience missing")
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, false); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, author, 1)
	if memberXP(t, author).Experience != xp {
		t.Fatal("accepted experience reversal missing")
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, true); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, author, 1)
}

func TestPointsAwardFailureRollsBackExperienceAndEvent(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	drainGrowth(t)
	_, _, err := testStore.CreateThread(ctx, fid, uid, "author", "atomic reward", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `CREATE FUNCTION points_fail_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected points failure'; END $$;CREATE TRIGGER points_fail_insert BEFORE INSERT ON points_ledger FOR EACH ROW EXECUTE FUNCTION points_fail_insert()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DROP TRIGGER IF EXISTS points_fail_insert ON points_ledger;DROP FUNCTION IF EXISTS points_fail_insert()`)
	})
	if _, err = testStore.ProcessMemberEvents(ctx, 1000); err == nil {
		t.Fatal("expected failed points write")
	}
	if memberXP(t, uid).Experience != 0 {
		t.Fatal("experience committed without points")
	}
	pointsBalance(t, uid, 0)
	if _, err = testPool.Exec(ctx, `DROP TRIGGER points_fail_insert ON points_ledger;DROP FUNCTION points_fail_insert()`); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	pointsBalance(t, uid, 1)
	if memberXP(t, uid).Experience != 5 {
		t.Fatal("event not retried")
	}
}
