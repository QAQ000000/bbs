// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func memberTestConfig(t *testing.T) MembershipConfig {
	t.Helper()
	ctx := context.Background()
	c, err := testStore.MembershipConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	t.Cleanup(func() {
		if _, err := testStore.pool.Exec(ctx, `UPDATE membership_config SET version=$1,body=$2 WHERE id`, c.Version, b); err != nil {
			t.Error(err)
		}
	})
	return c
}
func saveTestMemberConfig(t *testing.T, c MembershipConfig) {
	t.Helper()
	b, _ := json.Marshal(c)
	if _, err := testStore.pool.Exec(context.Background(), `UPDATE membership_config SET version=$1,body=$2 WHERE id`, c.Version, b); err != nil {
		t.Fatal(err)
	}
}
func drainGrowth(t *testing.T) {
	t.Helper()
	for i := 0; i < 100; i++ {
		n, err := testStore.ProcessMemberEvents(context.Background(), 1000)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("growth queue did not drain")
}
func memberXP(t *testing.T, uid int64) MemberState {
	t.Helper()
	m, err := testStore.Membership(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMembershipDefaultsAndValidation(t *testing.T) {
	c := memberTestConfig(t)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(c)
	b, _ := json.Marshal(DefaultMembershipConfig())
	if string(a) != string(b) {
		t.Fatal("SQL and Go defaults differ")
	}
	c.Levels[0].Permissions["admin.panel"] = true
	if c.Validate() == nil {
		t.Fatal("accepted management permission in member level")
	}
	delete(c.Levels[0].Permissions, "admin.panel")
	c.Levels[0].Badge.Icon = "<script>"
	if c.Validate() == nil {
		t.Fatal("accepted arbitrary badge markup")
	}
}
func TestMembershipAwardsReversalsAndNoFarming(t *testing.T) {
	ctx := context.Background()
	uid, other := setupUsers(t)
	fid := setupForum(t)
	th, p, err := testStore.CreateThread(ctx, fid, uid, "author", "member growth", "body", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if m := memberXP(t, uid); m.Experience != 0 {
		t.Fatal("pending content earned experience")
	}
	if err := testStore.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if m := memberXP(t, uid); m.Experience != 5 {
		t.Fatalf("approved thread xp=%d", m.Experience)
	}
	if _, _, err := testStore.LikeToggle(ctx, p.ID, other); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).Experience != 6 {
		t.Fatal("like not credited")
	}
	if _, _, err := testStore.LikeToggle(ctx, p.ID, other); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if _, _, err := testStore.LikeToggle(ctx, p.ID, other); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).Experience != 5 {
		t.Fatal("like toggle farmed experience")
	}
	digest := true
	if err := testStore.SetThreadProperties(ctx, th.ID, -1, &digest, nil); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).Experience != 25 {
		t.Fatal("digest not credited")
	}
	if _, _, err := testStore.DeletePost(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).Experience != 0 {
		t.Fatalf("delete did not reverse awards: %+v", memberXP(t, uid))
	}
	if err := testStore.RestoreThread(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).Experience != 0 {
		t.Fatal("restore farmed experience")
	}
	var sum int64
	if err := testStore.pool.QueryRow(ctx, `SELECT sum(delta) FROM member_experience WHERE user_id=$1`, uid).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != 0 {
		t.Fatal("ledger balance mismatch")
	}
}
func TestMembershipQueueTransactionAndRuleSnapshot(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	c := memberTestConfig(t)
	tx, err := testStore.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT member_enqueue($1,'thread','rollback-event',true)`, uid); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	drainGrowth(t)
	if memberXP(t, uid).Experience != 0 {
		t.Fatal("rolled back business event awarded")
	}
	for i := 0; i < 3; i++ {
		if _, _, err := testStore.CreateThread(ctx, fid, uid, "author", fmt.Sprint("event", i), "body", "", false, ""); err != nil {
			t.Fatal(err)
		}
	}
	c.Version++
	c.Rules["thread"] = GrowthRule{true, 100, 1000, true}
	saveTestMemberConfig(t, c)
	drainGrowth(t)
	if memberXP(t, uid).Experience != 15 {
		t.Fatal("queued events changed value after config update")
	}
	c.Rules["thread"] = GrowthRule{true, 5, 17, true}
	saveTestMemberConfig(t, c)
	if _, _, err := testStore.CreateThread(ctx, fid, uid, "author", "capped", "body", "", false, ""); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).Experience != 17 {
		t.Fatal("daily gross cap not enforced")
	}
}
func TestMembershipConcurrentQuotaAndActivity(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := testStore.ReserveMemberQuota(ctx, uid, "thread.create", 1, 5)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrMemberQuota) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 5 {
		t.Fatalf("quota admitted %d requests", successes.Load())
	}
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := testStore.RecordMemberActivity(ctx, uid); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	drainGrowth(t)
	m := memberXP(t, uid)
	if m.DaysVisited != 1 || m.Experience != 1 {
		t.Fatalf("daily activity duplicated: %+v", m)
	}
}
func TestMembershipUpgradeLockAndManualIdempotency(t *testing.T) {
	ctx := context.Background()
	uid, actor := setupUsers(t)
	c := memberTestConfig(t)
	if _, err := testStore.pool.Exec(ctx, `UPDATE users SET days_visited=20,posts_read=120,post_count=12 WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if err := testStore.UpgradeMember(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if memberXP(t, uid).LevelID != 0 {
		t.Fatal("legacy metrics bypassed experience requirements")
	}
	if _, err := testStore.pool.Exec(ctx, `UPDATE member_states SET experience=5000 WHERE user_id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if err := testStore.UpgradeMember(ctx, uid); err != nil {
		t.Fatal(err)
	}
	m := memberXP(t, uid)
	if m.LevelID != 4 {
		t.Fatal("did not skip directly to highest eligible level")
	}
	lid, locked := 0, true
	a := MemberAdjustment{Version: m.Version, LevelID: &lid, Locked: &locked, Delta: 20, Reason: "support correction", Key: "manual-test-key"}
	if err := testStore.AdjustMember(ctx, uid, actor, a); err != nil {
		t.Fatal(err)
	}
	if err := testStore.AdjustMember(ctx, uid, actor, a); err != nil {
		t.Fatal("retry was not idempotent", err)
	}
	if err := testStore.UpgradeMember(ctx, uid); err != nil {
		t.Fatal(err)
	}
	m = memberXP(t, uid)
	if m.LevelID != 0 || m.Experience != 5020 {
		t.Fatal("locked level overwritten or award duplicated")
	}
	a.Delta = 21
	if !errors.Is(testStore.AdjustMember(ctx, uid, actor, a), ErrMembershipConflict) {
		t.Fatal("idempotency key accepted different payload")
	}
	var role int
	if err := testStore.pool.QueryRow(ctx, `SELECT group_id FROM users WHERE id=$1`, uid).Scan(&role); err != nil || role != 0 {
		t.Fatal("growth changed management role", err)
	}
	_ = c
}
func TestMembershipPreviewConflictsAndNoImplicitDemotion(t *testing.T) {
	ctx := context.Background()
	c := memberTestConfig(t)
	uid, actor := setupUsers(t)
	c.Levels[0].Name = "见习会员"
	p, err := testStore.PreviewMembership(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := testStore.RecordMemberActivity(ctx, uid); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if !errors.Is(testStore.ApplyMembership(ctx, c, p.Token, actor), ErrMembershipConflict) {
		t.Fatal("stale preview applied")
	}
	p, err = testStore.PreviewMembership(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = testStore.ApplyMembership(ctx, c, p.Token, actor); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(testStore.ApplyMembership(ctx, c, p.Token, actor), ErrMembershipConflict) {
		t.Fatal("config version replay accepted")
	}
	m := memberXP(t, uid)
	lid := 2
	if err = testStore.AdjustMember(ctx, uid, actor, MemberAdjustment{Version: m.Version, LevelID: &lid, Reason: "manual promotion", Key: "promote-manual"}); err != nil {
		t.Fatal(err)
	}
	if err = testStore.UpgradeMember(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if memberXP(t, uid).LevelID != 2 {
		t.Fatal("automatic evaluation demoted user")
	}
	c.Version++
	c.Levels = c.Levels[:2]
	if _, err := testStore.PreviewMembership(ctx, c); err == nil {
		t.Fatal("removed an in-use level")
	}
}
func TestMembershipReadDeduplicatesConcurrentPosts(t *testing.T) {
	ctx := context.Background()
	author, reader := setupUsers(t)
	fid := setupForum(t)
	th, p, err := testStore.CreateThread(ctx, fid, author, "author", "read", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := testStore.RecordMemberRead(ctx, reader, p.ID, th.ID, 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if memberXP(t, reader).PostsRead != 1 {
		t.Fatal("floor gaps or concurrent repeats credited multiple reads")
	}
}

func TestMembershipDuplicateQueueAndBanFreeze(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	for i := 0; i < 20; i++ {
		if _, err := testStore.pool.Exec(ctx, `SELECT member_enqueue($1,'thread','same-business-event',true)`, uid); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := testStore.ProcessMemberEvents(ctx, 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	drainGrowth(t)
	if memberXP(t, uid).Experience != 5 {
		t.Fatal("duplicate queue events awarded more than once")
	}
	if _, err := testStore.pool.Exec(ctx, `UPDATE users SET days_visited=20,posts_read=120,post_count=12,banned_until='infinity' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.pool.Exec(ctx, `UPDATE member_states SET experience=500 WHERE user_id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).LevelID != 0 {
		t.Fatal("banned account automatically promoted")
	}
	if _, err := testStore.pool.Exec(ctx, `UPDATE users SET banned_until=NULL WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	drainGrowth(t)
	if memberXP(t, uid).LevelID != 2 {
		t.Fatal("unban eligibility event was lost")
	}
}
