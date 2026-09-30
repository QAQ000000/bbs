package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPlainExcerptAndCover(t *testing.T) {
	md := "# 标题" + "\n\n" + "第一段 **正文**。" + "\n\n" + "```go" + "\n" + "code()" + "\n" + "```" + "\n\n" + "![图](/uploads/a.png)" + "\n\n" + "[链接](https://example.com)"
	got := PlainExcerpt(md, 6)
	if got == "" || strings.ContainsAny(got, "#*[]()") {
		t.Fatalf("markdown not stripped: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if len([]rune(got)) > 7 {
		t.Fatalf("excerpt too long: %q", got)
	}
	if FirstCoverURL(md) != "/uploads/a.png" {
		t.Fatalf("cover not extracted: %q", FirstCoverURL(md))
	}
	if FirstCoverURL("![x](https://evil.example/a.png)") != "" {
		t.Fatal("external cover must be rejected")
	}
	if FirstCoverURL("![x](/uploads/a.txt)") != "" {
		t.Fatal("non-image cover must be rejected")
	}
	if FirstCoverURL("![x](/uploads/b.JPEG)") != "/uploads/b.JPEG" {
		t.Fatal("extension matching must accept jpeg case")
	}
}

func TestFollowingFeedsFilterOrderCursorAndPreviews(t *testing.T) {
	ctx := context.Background()
	a, b := setupUsers(t)
	fid := setupForum(t)
	ctx = WithVisibleForums(ctx, []int64{fid})
	if _, err := testStore.SaveSubscription(ctx, a, Subscription{Kind: "forum", TargetID: fid}, true); err != nil {
		t.Fatal(err)
	}
	if err := testStore.Follow(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStore.CreateThread(ctx, fid, b, "b", "B1", "![封面](/uploads/cover.png) 第一篇摘要内容", "", false, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, _, err := testStore.CreateThread(ctx, fid, b, "b", "B2", "第二篇摘要内容", "", false, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, _, err := testStore.CreateThread(ctx, fid, a, "a", "A1", "自己发的主题", "", false, ""); err != nil {
		t.Fatal(err)
	}

	// 版块流：订阅版块内全部主题，按发布时间与 ID 倒序。
	page1, more, err := testStore.FollowingForumsFeed(ctx, a, 2, nil)
	if err != nil || len(page1) != 2 || !more {
		t.Fatalf("forum feed page1: n=%d more=%v err=%v", len(page1), more, err)
	}
	if page1[0].Title != "A1" || page1[1].Title != "B2" {
		t.Fatalf("forum order: %s,%s", page1[0].Title, page1[1].Title)
	}
	last := page1[len(page1)-1]
	cursor := &FeedCursor{UserID: a, Stream: "forums", Sort: "created", CreatedAt: last.CreatedAt, ID: last.ID}
	page2, more2, err := testStore.FollowingForumsFeed(ctx, a, 2, cursor)
	if err != nil || len(page2) != 1 || page2[0].Title != "B1" || more2 {
		t.Fatalf("forum feed page2: n=%d more=%v err=%v", len(page2), more2, err)
	}

	// 人的流：只收录被关注者作为主题作者发布的主题。
	userRows, _, err := testStore.FollowingUsersFeed(ctx, a, 10, nil)
	if err != nil || len(userRows) != 2 {
		t.Fatalf("user feed: n=%d err=%v", len(userRows), err)
	}
	var b1ID int64
	for _, row := range userRows {
		if row.AuthorID != b {
			t.Fatalf("user feed leaked author %d", row.AuthorID)
		}
		if row.Title == "B1" {
			b1ID = row.ID
		}
	}

	// 游标必须绑定账号与流类型。
	if _, _, err := testStore.FollowingForumsFeed(ctx, b, 2, cursor); err != ErrFeedCursorInvalid {
		t.Fatalf("cross-account cursor accepted: %v", err)
	}
	if _, _, err := testStore.FollowingUsersFeed(ctx, a, 2, cursor); err != ErrFeedCursorInvalid {
		t.Fatalf("cross-stream cursor accepted: %v", err)
	}
	if _, _, err := testStore.FollowingForumsFeed(ctx, a, 2, &FeedCursor{UserID: a, Stream: "forums", Sort: "hot", CreatedAt: last.CreatedAt, ID: last.ID}); err != ErrFeedCursorInvalid {
		t.Fatalf("wrong sort cursor accepted: %v", err)
	}

	// 通知关闭或静音不等于取消订阅。
	if _, err := testPool.Exec(ctx, `UPDATE forum_subscriptions SET enabled=false,notify_in_app=false,notify_email=false,muted_until=now()+interval '1 day' WHERE uid=$1 AND forum_id=$2`, a, fid); err != nil {
		t.Fatal(err)
	}
	muted, _, err := testStore.FollowingForumsFeed(ctx, a, 10, nil)
	if err != nil || len(muted) != 3 {
		t.Fatalf("muted subscription dropped from feed: n=%d err=%v", len(muted), err)
	}
	if n, err := testStore.FollowingForumCount(ctx, a); err != nil || n != 1 {
		t.Fatalf("forum count after mute: n=%d err=%v", n, err)
	}

	// 摘要与封面：一次批量投影。
	previews, err := testStore.ThreadPreviews(ctx, []int64{b1ID})
	if err != nil {
		t.Fatal(err)
	}
	pv, ok := previews[b1ID]
	if !ok || !strings.Contains(pv.Excerpt, "第一篇摘要内容") || pv.CoverURL != "/uploads/cover.png" {
		t.Fatalf("preview projection wrong: %+v ok=%v", pv, ok)
	}
	if strings.Contains(pv.Excerpt, "/uploads/") {
		t.Fatalf("excerpt leaked markdown url: %q", pv.Excerpt)
	}
}
