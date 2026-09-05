// SPDX-License-Identifier: AGPL-3.0-or-later

// security_test.go：安全整改批次回归（楼层号复用、恢复泄露、重置令牌原子性、
// 邮箱验证绑定、永久禁言扫描、审核计数对称、版本并发、点赞并发、公开口径）。

package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestFloorSeqNoReuse：删除中间楼层后新回复不得复用已删除楼层号（防重复公开楼层）。
func TestFloorSeqNoReuse(t *testing.T) {
	ctx := context.Background()
	uid1, uid2 := setupUsers(t)
	fid := setupForum(t)

	th, p1, err := testStore.CreateThread(ctx, fid, uid1, "u1", "楼层复用回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p2, err := testStore.CreateReply(ctx, th.ID, uid2, "u2", "二楼", "<p>二楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p3, err := testStore.CreateReply(ctx, th.ID, uid2, "u2", "三楼", "<p>三楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Floor != 2 || p3.Floor != 3 {
		t.Fatalf("初始楼层号应为 2/3: %d/%d", p2.Floor, p3.Floor)
	}
	if _, _, err := testStore.DeletePost(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	_, p4, err := testStore.CreateReply(ctx, th.ID, uid2, "u2", "四楼", "<p>四楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if p4.Floor <= p3.Floor {
		t.Fatalf("新回复楼层号 %d 不应复用已删除的 2 楼（应 > %d）", p4.Floor, p3.Floor)
	}
	// 公开楼层号两两不重复
	posts, err := testStore.Posts(ctx, th.ID, 1, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, p := range posts {
		if seen[p.Floor] {
			t.Fatalf("公开楼层号重复: %d", p.Floor)
		}
		seen[p.Floor] = true
	}
	_ = p1
}

// TestRestoreKeepsIndividualDeletes：单独删除的楼层不随主题恢复重新公开。
func TestRestoreKeepsIndividualDeletes(t *testing.T) {
	ctx := context.Background()
	uid1, uid2 := setupUsers(t)
	fid := setupForum(t)

	th, p1, err := testStore.CreateThread(ctx, fid, uid1, "u1", "恢复批次回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p2, err := testStore.CreateReply(ctx, th.ID, uid2, "u2", "回复A", "<p>回复A</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = testStore.CreateReply(ctx, th.ID, uid2, "u2", "回复B", "<p>回复B</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	// 先单独删除回复A，再删除整个主题，最后恢复
	if _, _, err := testStore.DeletePost(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	deleted, _, err := testStore.DeletePost(ctx, p1.ID)
	if err != nil || !deleted {
		t.Fatalf("删首楼应软删主题: %v %v", deleted, err)
	}
	if err := testStore.RestoreThread(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	posts, err := testStore.Posts(ctx, th.ID, 1, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range posts {
		if p.ID == p2.ID {
			t.Fatal("单独删除的回复不应随主题恢复重新公开")
		}
	}
	if len(posts) != 2 {
		t.Fatalf("恢复后公开楼层数应为 2（首楼+回复B）: %d", len(posts))
	}
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT post_count FROM threads WHERE id=$1`, th.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("恢复后 post_count 应重算为 2: %d", count)
	}
}

// TestPurgeAfterRestore：恢复后的主题再被彻底删除应拒绝（并发恢复/清空只成一个）。
func TestPurgeAfterRestore(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	fid := setupForum(t)
	th, p1, err := testStore.CreateThread(ctx, fid, uid1, "u1", "恢复后清除回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStore.DeletePost(ctx, p1.ID); err != nil {
		t.Fatal(err)
	}
	if err := testStore.RestoreThread(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if err := testStore.PurgeThread(ctx, th.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("恢复后的主题应拒绝彻底删除: %v", err)
	}
	if _, err := testStore.Thread(ctx, th.ID); err != nil {
		t.Fatal("主题应仍然存在")
	}
}

// TestResetTokenAtomic：同一令牌只能重置一次；其余令牌随首次成功全部作废。
func TestResetTokenAtomic(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	raw1, err := testStore.CreatePasswordReset(ctx, uid1)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := testStore.CreatePasswordReset(ctx, uid1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.ResetPasswordByToken(ctx, raw1, "newpassword1"); err != nil {
		t.Fatal(err)
	}
	// 第二个令牌已被首次成功作废
	if _, err := testStore.ResetPasswordByToken(ctx, raw2, "newpassword2"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("其余重置令牌应作废: %v", err)
	}
	// 同一令牌二次使用同样失败
	if _, err := testStore.ResetPasswordByToken(ctx, raw1, "newpassword3"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("令牌不得重复使用: %v", err)
	}
	u, err := testStore.UserByID(ctx, uid1)
	if err != nil {
		t.Fatal(err)
	}
	if !testStore.VerifyPassword(u, "newpassword1") {
		t.Fatal("密码应为第一次重置的结果")
	}
}

// TestEmailVerifyBoundToEmail：换绑邮箱后旧验证令牌不能验证新邮箱。
func TestEmailVerifyBoundToEmail(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	if _, err := testStore.UpdateProfile(ctx, uid1, "", "a@test.local"); err != nil {
		t.Fatal(err)
	}
	raw, err := testStore.CreateEmailVerify(ctx, uid1, "a@test.local")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := testStore.UpdateProfile(ctx, uid1, "", "b@test.local")
	if err != nil || !changed {
		t.Fatalf("换绑邮箱应返回变更: %v %v", changed, err)
	}
	// 换绑即作废旧令牌（清除验证令牌行）
	if _, _, err := testStore.ConsumeEmailVerify(ctx, raw); !errors.Is(err, ErrNotFound) {
		t.Fatalf("换绑后旧令牌应已作废: %v", err)
	}
	u, err := testStore.UserByID(ctx, uid1)
	if err != nil {
		t.Fatal(err)
	}
	if u.EmailVerified {
		t.Fatal("换绑后验证状态应为 false")
	}
	// 用新邮箱重新申请后可验证
	raw2, err := testStore.CreateEmailVerify(ctx, uid1, "b@test.local")
	if err != nil {
		t.Fatal(err)
	}
	if _, v2, err := testStore.ConsumeEmailVerify(ctx, raw2); err != nil || !v2 {
		t.Fatalf("新邮箱令牌应验证成功: %v", err)
	}
}

// TestPermanentBanScannable：永久禁言/封禁不得使用 infinity（驱动扫描失败）。
func TestPermanentBanScannable(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	if err := testStore.BanUser(ctx, uid1, 0, "测试永久禁言"); err != nil {
		t.Fatal(err)
	}
	banned, until, reason := testStore.IsBanned(ctx, uid1)
	if !banned || reason != "测试永久禁言" || !until.After(time.Now()) {
		t.Fatalf("永久禁言应生效: %v %v %q", banned, until, reason)
	}
	if err := testStore.UnbanUser(ctx, uid1); err != nil {
		t.Fatal(err)
	}
	if err := testStore.BlockUser(ctx, uid1, 0); err != nil {
		t.Fatal(err)
	}
	u, err := testStore.UserByID(ctx, uid1)
	if err != nil {
		t.Fatalf("封禁用户读取失败（infinity 扫描）: %v", err)
	}
	if !u.IsBlocked() {
		t.Fatal("永久封禁应生效")
	}
	// 用户列表扫描同样不得失败
	if _, _, err := testStore.SearchUsers(ctx, UserQuery{Page: 1, Size: 50}); err != nil {
		t.Fatalf("用户列表扫描失败: %v", err)
	}
	if err := testStore.UnblockUser(ctx, uid1); err != nil {
		t.Fatal(err)
	}
}

// TestReEnqueueCountSymmetry：重新入队扣计数、批准加回，反复操作不抬升。
func TestReEnqueueCountSymmetry(t *testing.T) {
	ctx := context.Background()
	uid1, uid2 := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, uid1, "u1", "计数对称回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	countOf := func(uid int64) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT post_count FROM users WHERE id=$1`, uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	_, rp, err := testStore.CreateReply(ctx, th.ID, uid2, "u2", "回复", "<p>回复</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	base := countOf(uid2)
	if base < 1 {
		t.Fatalf("回复后计数应增加: %d", base)
	}
	if err := testStore.SetPostPendingModeration(ctx, rp.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	if n := countOf(uid2); n != base-1 {
		t.Fatalf("重新入队应扣减计数 %d → %d", base, n)
	}
	if _, _, err := testStore.SetPostApproved(ctx, rp.ID); err != nil {
		t.Fatal(err)
	}
	if n := countOf(uid2); n != base {
		t.Fatalf("批准后计数应还原: %d != %d", n, base)
	}
	// 再来一轮：重复编辑/批准不能持续抬升
	if err := testStore.SetPostPendingModeration(ctx, rp.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStore.SetPostApproved(ctx, rp.ID); err != nil {
		t.Fatal(err)
	}
	if n := countOf(uid2); n != base {
		t.Fatalf("第二轮批准后计数仍应等于基线: %d != %d", n, base)
	}
}

// TestForumStatsExcludePendingThread：首楼重新入队后版块统计不得引用待审主题。
func TestForumStatsExcludePendingThread(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	fid := setupForum(t)
	_, p1, err := testStore.CreateThread(ctx, fid, uid1, "u1", "待审统计回归主题", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := testStore.RecomputeForumStats(ctx, fid); err != nil {
		t.Fatal(err)
	}
	f, err := testStore.Forum(ctx, fid)
	if err != nil {
		t.Fatal(err)
	}
	if f.LastThreadTitle != "待审统计回归主题" {
		t.Fatalf("公开统计应引用该主题: %q", f.LastThreadTitle)
	}
	if err := testStore.SetPostPendingModeration(ctx, p1.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := testStore.RecomputeForumStats(ctx, fid); err != nil {
		t.Fatal(err)
	}
	f, err = testStore.Forum(ctx, fid)
	if err != nil {
		t.Fatal(err)
	}
	if f.LastThreadTitle == "待审统计回归主题" {
		t.Fatal("待审主题不得出现在公开统计的最后发表中")
	}
	if f.ThreadCount != 0 {
		t.Fatalf("待审主题不计入主题数: %d", f.ThreadCount)
	}
}

// TestEditVersionConflict：过期版本提交被拒，历史快照不落盘。
func TestEditVersionConflict(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	fid := setupForum(t)
	th, p1, err := testStore.CreateThread(ctx, fid, uid1, "u1", "版本冲突回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = testStore.UpdatePost(ctx, p1.ID, p1.Version+5, uid1, p1.ContentMD, "新标题", "新内容", "<p>新内容</p>")
	if !errors.Is(err, ErrEditConflict) {
		t.Fatalf("过期版本应报冲突: %v", err)
	}
	if n := testStore.PostEditCount(ctx, p1.ID); n != 0 {
		t.Fatalf("冲突提交不应留下历史快照: %d", n)
	}
	p2, th2, err := testStore.UpdatePost(ctx, p1.ID, p1.Version, uid1, p1.ContentMD, "新标题", "新内容", "<p>新内容</p>")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Version != p1.Version+1 || th2.Title != "新标题" || th2.ID != th.ID {
		t.Fatalf("正确版本应编辑成功: v%d %q", p2.Version, th2.Title)
	}
	if n := testStore.PostEditCount(ctx, p1.ID); n != 1 {
		t.Fatalf("成功编辑应留 1 份快照: %d", n)
	}
}

// TestLikeToggleConcurrent：并发点赞后计数与动作行数一致（不因竞争写小）。
func TestLikeToggleConcurrent(t *testing.T) {
	ctx := context.Background()
	uid1, uid2 := setupUsers(t)
	fid := setupForum(t)
	_, p1, err := testStore.CreateThread(ctx, fid, uid1, "u1", "并发点赞回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, _, errs[0] = testStore.LikeToggle(ctx, p1.ID, uid1) }()
	go func() { defer wg.Done(); _, _, errs[1] = testStore.LikeToggle(ctx, p1.ID, uid2) }()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("点赞 %d 失败: %v", i, err)
		}
	}
	var actions int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM post_actions WHERE pid=$1 AND action=1`, p1.ID).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	p, err := testStore.Post(ctx, p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.LikeCount != actions {
		t.Fatalf("点赞计数应与动作行数一致: count=%d actions=%d", p.LikeCount, actions)
	}
}

// TestMoveThreadStatsBothSides：移帖后新旧版块统计各自正确。
func TestMoveThreadStatsBothSides(t *testing.T) {
	ctx := context.Background()
	uid1, _ := setupUsers(t)
	fidA := setupForum(t)
	var fidB int64
	if err := testPool.QueryRow(ctx,
		`INSERT INTO forums (category_id, name) VALUES (1,'移帖目标版块') RETURNING id`).Scan(&fidB); err != nil {
		t.Fatal(err)
	}
	th, _, err := testStore.CreateThread(ctx, fidA, uid1, "u1", "移帖统计回归", "首楼", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := testStore.RecomputeForumStats(ctx, fidA); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.MoveThread(ctx, th.ID, fidB); err != nil {
		t.Fatal(err)
	}
	fa, err := testStore.Forum(ctx, fidA)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := testStore.Forum(ctx, fidB)
	if err != nil {
		t.Fatal(err)
	}
	if fa.ThreadCount != 0 || fa.LastThreadTitle != "" {
		t.Fatalf("原版块统计应清空: threads=%d last=%q", fa.ThreadCount, fa.LastThreadTitle)
	}
	if fb.ThreadCount != 1 || fb.LastThreadTitle != "移帖统计回归" {
		t.Fatalf("目标版块统计应含主题: threads=%d last=%q", fb.ThreadCount, fb.LastThreadTitle)
	}
}
