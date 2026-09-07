// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestWorkflowUnreadAfterDeletedFloor(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, owner, "owner", "unread", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := testStore.CreateReply(ctx, th.ID, other, "other", "second", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, third, err := testStore.CreateReply(ctx, th.ID, other, "other", "third", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.FavoriteToggle(ctx, owner, th.ID); err != nil {
		t.Fatal(err)
	}
	if err = testStore.RecordMemberRead(ctx, owner, third.ID, th.ID, third.Floor); err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.DeletePost(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	_, fourth, err := testStore.CreateReply(ctx, th.ID, other, "other", "fourth", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		rows, total, err := testStore.FavoritesOfUser(ctx, owner, 1, 20)
		if err != nil || total != 1 || len(rows) != 1 || rows[0].HasNew != want {
			t.Fatal(rows, total, err)
		}
		n := testStore.UnreadFavorites(ctx, owner)
		if (n == 1) != want {
			t.Fatal("unread count", n, want)
		}
	}
	check(true)
	if err = testStore.SetPostPendingModeration(ctx, fourth.ID, "review"); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, _, err = testStore.SetPostApproved(ctx, fourth.ID); err != nil {
		t.Fatal(err)
	}
	check(true)
	if _, _, err = testStore.DeletePost(ctx, fourth.ID); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestWorkflowNotificationVisibilityDedupAndRead(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	fid := setupForum(t)
	th, p, err := testStore.CreateThread(ctx, fid, owner, "owner", "source", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	row := &Notification{UID: other, FromUID: owner, Type: "reply", ThreadID: th.ID, PostID: p.ID, EventKey: "source-once"}
	if err = testStore.AddNotifications(ctx, []*Notification{row}); err != nil || row.ID == 0 {
		t.Fatal(err)
	}
	firstID := row.ID
	if err = testStore.AddNotifications(ctx, []*Notification{row}); err != nil || row.ID != 0 {
		t.Fatal("duplicate", err, row.ID)
	}
	if n, err := testStore.NotificationUnreadCount(ctx, other); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := testStore.ReadNotifications(ctx, owner, []int64{firstID}, false); err != nil || n != 0 {
		t.Fatal("cross user mark", n, err)
	}
	scoped := WithVisibleForums(ctx, []int64{})
	if n, err := testStore.NotificationUnreadCount(scoped, other); err != nil || n != 0 {
		t.Fatal("hidden count", n, err)
	}
	if n, err := testStore.ReadNotifications(scoped, other, nil, true); err != nil || n != 0 {
		t.Fatal("hidden mark", n, err)
	}
	if _, _, err = testStore.DeletePost(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if rows, total, err := testStore.NotificationPage(ctx, other, 1, 30, false); err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("deleted notification visible", total, err)
	}
}

func TestWorkflowEventNotifications(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, owner, "owner", "events", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := testStore.CreateReply(ctx, th.ID, other, "other", "answer", "", true, "review")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.SetPostApproved(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, true); err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, false); err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetAcceptedReply(ctx, th.ID, p.ID, owner, true); err != nil {
		t.Fatal(err)
	}
	c := testTitle(t, owner, TitleCondition{Metric: "accepted_replies", Target: 1, ForumID: fid})
	drainTitles(t)
	if err = testStore.AdjustTitle(ctx, other, c.ID, owner, TitleAdjustment{Action: "revoke", Reason: "test", Key: "workflow-revoke", Version: c.Version}); err != nil {
		t.Fatal(err)
	}
	if err = testStore.CreateReport(ctx, p.ID, owner, "review please"); err != nil {
		t.Fatal(err)
	}
	var rid int64
	if err = testPool.QueryRow(ctx, `SELECT id FROM reports WHERE post_id=$1 AND reporter=$2`, p.ID, owner).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE users SET group_id=1 WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = testStore.HandleReport(ctx, rid, owner, "dismiss", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `INSERT INTO member_changes(user_id,action,detail) VALUES($1,'level.upgrade','{"to":4}')`, other); err != nil {
		t.Fatal(err)
	}
	rows, _, err := testStore.NotificationPage(ctx, other, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, n := range rows {
		kinds[n.Type]++
	}
	for _, kind := range []string{"moderation.approved", "reply.accepted", "title.granted", "title.revoked", "membership.upgraded"} {
		if kinds[kind] != 1 {
			t.Fatal("event notification missing or duplicate", kind, kinds)
		}
	}
	if _, _, err = testStore.DeletePost(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	rows, _, err = testStore.NotificationPage(ctx, other, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rows {
		if n.Type == "reply.accepted" {
			t.Fatal("deleted acceptance visible")
		}
	}
	rows, _, err = testStore.NotificationPage(ctx, owner, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range rows {
		if n.Type == "report.dismissed" {
			found = true
		}
	}
	if !found {
		t.Fatal("report result missing")
	}
}

func TestWorkflowReplyPositionAndOwnParentPrivacy(t *testing.T) {
	ctx := context.Background()
	owner, other := setupUsers(t)
	fid := setupForum(t)
	th, first, err := testStore.CreateThread(ctx, fid, owner, "owner", "parent secret", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := testStore.CreateReply(ctx, th.ID, other, "other", "second", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, third, err := testStore.CreateReplyTo(ctx, th.ID, other, "other", "third", "", false, "", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.DeletePost(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	page, err := testStore.PostPosition(ctx, third, other, false, 2)
	if err != nil || page != 1 {
		t.Fatal("floor gap position", page, err)
	}
	if _, _, err = testStore.CreateReplyTo(ctx, th.ID, other, "other", "invalid", "", false, "", second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted target accepted", err)
	}
	states, err := testStore.PostViewerStates(ctx, []int64{third.ID}, other)
	if err != nil || states[third.ID].ReplyTo == nil || states[third.ID].ReplyTo.Available {
		t.Fatal(states, err)
	}
	if err = testStore.SetPostPendingModeration(ctx, first.ID, "hidden parent"); err != nil {
		t.Fatal(err)
	}
	rows, total, err := testStore.OwnContentPage(ctx, other, "replies", "pending", 1, 30)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatal(rows, total, err)
	}
	if rows[0].Subject != "" || rows[0].ParentAvailable {
		t.Fatal("hidden parent metadata leaked", rows)
	}
}

func TestWorkflowConcurrentThreadApproval(t *testing.T) {
	ctx := context.Background()
	owner, _ := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, owner, "owner", "approve once", "body", "", true, "review")
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err = testPool.QueryRow(ctx, `SELECT post_count FROM users WHERE id=$1`, owner).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- testStore.SetThreadApproved(ctx, th.ID) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var after, count int
	if err = testPool.QueryRow(ctx, `SELECT post_count,(SELECT count(*) FROM notifications WHERE uid=$1 AND type='moderation.approved') FROM users WHERE id=$1`, owner).Scan(&after, &count); err != nil || after != before+1 || count != 1 {
		t.Fatal(before, after, count, err)
	}
}
