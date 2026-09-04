// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"testing"
)

func TestDeleteUserWithSoftDeletedPosts(t *testing.T) {
	ctx := context.Background()
	author, replier := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, author, "作者", "删号主题", "首楼", "<p>x</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := testStore.CreateReply(ctx, th.ID, replier, "回复者", "待软删", "<p>y</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStore.DeletePost(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := testStore.DeleteUser(ctx, replier); err != nil {
		t.Fatalf("仅剩软删楼层应可删号: %v", err)
	}
	var n int64
	if err := testStore.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, replier).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("用户应已删除")
	}
	if err := testStore.pool.QueryRow(ctx, `SELECT count(*) FROM posts WHERE author_id=$1`, replier).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("软删楼层应一并硬删: %d", n)
	}
	if err := testStore.DeleteUser(ctx, author); err == nil {
		t.Fatal("仍有公开主题的用户不应被删号")
	} else if err != ErrUserHasContent {
		t.Fatalf("期望 ErrUserHasContent，得到 %v", err)
	}
}
