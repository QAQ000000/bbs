// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMessagesConcurrentFirstAndBlock(t *testing.T) {
	ctx := context.Background()
	a, b := setupUsers(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	msgs := make(chan Message, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := testStore.SendDirectMessage(ctx, a, b, "hello")
			results <- err
			if err == nil {
				msgs <- m
			}
		}()
	}
	wg.Wait()
	close(results)
	close(msgs)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrMessageReplyRequired) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("first message bypass", success)
	}
	first := <-msgs
	if err := testStore.Follow(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if err := testStore.SetConversationBlocked(ctx, first.ConversationID, b, true); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.SendDirectMessage(ctx, a, b, "blocked"); !errors.Is(err, ErrMessageBlocked) {
		t.Fatal(err)
	}
	if err := testStore.Follow(ctx, a, b); !errors.Is(err, ErrMessageBlocked) {
		t.Fatal("block follow bypass", err)
	}
	rows, total, err := testStore.FollowUsers(ctx, a, false, 1)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal(rows, total, err)
	}
	if err = testStore.SetConversationBlocked(ctx, first.ConversationID, b, false); err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.SendDirectMessage(ctx, a, b, "still waiting"); !errors.Is(err, ErrMessageReplyRequired) {
		t.Fatal(err)
	}
	response, err := testStore.SendDirectMessage(ctx, b, a, "hello back")
	if err != nil || response.ConversationID != first.ConversationID {
		t.Fatal(response, err)
	}
	if _, err = testStore.SendDirectMessage(ctx, a, b, "unlocked"); err != nil {
		t.Fatal(err)
	}
	conversations, n, err := testStore.Conversations(ctx, b, 1)
	if err != nil || n != 1 || conversations[0].Unread != 2 {
		t.Fatal(conversations, n, err)
	}
	if err = testStore.ReadConversation(ctx, b, first.ConversationID, response.ID); err != nil {
		t.Fatal(err)
	}
	conversations, _, err = testStore.Conversations(ctx, b, 1)
	if err != nil || conversations[0].Unread != 1 {
		t.Fatal(conversations, err)
	}
	if _, err = testStore.ConversationMessages(ctx, 999999, first.ConversationID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatal("outsider read", err)
	}
	if err = testStore.SetConversationBlocked(ctx, first.ConversationID, 999999, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("outsider block", err)
	}
	if err = testStore.ReadConversation(ctx, b, first.ConversationID, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatal("invalid read marker", err)
	}
}

func TestMessagesSimultaneousOppositeSendAndCursor(t *testing.T) {
	ctx := context.Background()
	a, b := setupUsers(t)
	var wg sync.WaitGroup
	out := make(chan Message, 2)
	errs := make(chan error, 2)
	for _, pair := range [][2]int64{{a, b}, {b, a}} {
		wg.Add(1)
		go func(pair [2]int64) {
			defer wg.Done()
			m, err := testStore.SendDirectMessage(ctx, pair[0], pair[1], "simultaneous")
			out <- m
			errs <- err
		}(pair)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	x, y := <-out, <-out
	if x.ConversationID != y.ConversationID {
		t.Fatal("duplicate conversation", x, y)
	}
	rows, err := testStore.ConversationMessages(ctx, a, x.ConversationID, 0)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	before := rows[0].ID
	rows, err = testStore.ConversationMessages(ctx, a, x.ConversationID, before)
	if err != nil || len(rows) != 1 || rows[0].ID >= before {
		t.Fatal(rows, err)
	}
	if _, err = testStore.SendDirectMessage(ctx, a, 999999, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = testStore.SendDirectMessage(ctx, a, a, "self"); !errors.Is(err, ErrCommunityInvalid) {
		t.Fatal(err)
	}
	if _, err = testStore.SendDirectMessage(ctx, a, b, "  "); !errors.Is(err, ErrCommunityInvalid) {
		t.Fatal(err)
	}
}

func TestTagsAtomicWritesAliasesAndVisibility(t *testing.T) {
	ctx := context.Background()
	a, _ := setupUsers(t)
	fid := setupForum(t)
	g, err := testStore.SaveTag(ctx, Tag{Name: " Go ", Slug: "community-go", Status: "active"}, a)
	if err != nil {
		t.Fatal(err)
	}
	th, first, err := testStore.CreateTaggedThread(ctx, fid, a, "author", "tagged", "body", "", false, "", []int64{g.ID})
	if err != nil {
		t.Fatal(err)
	}
	version, err := testStore.SetThreadTags(ctx, th.ID, first.Version, nil)
	if err != nil || version != 2 {
		t.Fatal(version, err)
	}
	if _, err = testStore.SetThreadTags(ctx, th.ID, first.Version, []int64{g.ID}); !errors.Is(err, ErrCommunityConflict) {
		t.Fatal(err)
	}
	if _, err = testStore.SetThreadTags(ctx, th.ID, version, []int64{g.ID, g.ID}); !errors.Is(err, ErrCommunityInvalid) {
		t.Fatal(err)
	}
	if _, err = testStore.SetThreadTags(ctx, th.ID, version, []int64{g.ID}); err != nil {
		t.Fatal(err)
	}
	g.Slug = "community-golang"
	g, err = testStore.SaveTag(ctx, g, a)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := testStore.ResolveTag(ctx, "community-go"); err != nil || id != g.ID {
		t.Fatal(id, err)
	}
	if _, err = testStore.SaveTag(ctx, Tag{Name: "different", Slug: "community-go", Status: "active"}, a); !errors.Is(err, ErrCommunityConflict) {
		t.Fatal(err)
	}
	scoped := WithVisibleForums(ctx, []int64{})
	_, total, err := testStore.TaggedThreads(scoped, g.ID, 1, 30)
	if err != nil || total != 0 {
		t.Fatal("restricted tag filter", total, err)
	}
	tags, _, err := testStore.Tags(scoped, 1, false, "go")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range tags {
		if v.ThreadCount != 0 {
			t.Fatal("restricted count", v)
		}
	}
	g.Status = "disabled"
	g, err = testStore.SaveTag(ctx, g, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.CreateTaggedThread(ctx, fid, a, "author", "invalid disabled", "body", "", false, "", []int64{g.ID}); !errors.Is(err, ErrCommunityInvalid) {
		t.Fatal(err)
	}
	var n int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE author_id=$1`, a).Scan(&n); err != nil || n != 1 {
		t.Fatal("invalid tags left partial thread", n, err)
	}
}

func TestSubscriptionsDefaultsAndDurableBatch(t *testing.T) {
	ctx := context.Background()
	a, b := setupUsers(t)
	fid := setupForum(t)
	sub, err := testStore.SaveSubscription(ctx, b, Subscription{Kind: "forum", TargetID: fid}, true)
	if err != nil || !sub.Enabled || !sub.NotifyEmail || !sub.NotifyInApp {
		t.Fatal(sub, err)
	}
	_, p, err := testStore.CreateThread(ctx, fid, a, "author", "subscription new", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	failOnce := true
	calls := 0
	policy := func(uid, pid int64) (string, bool, bool, error) {
		if pid == p.ID {
			calls++
			if failOnce {
				return "", false, false, fmt.Errorf("temporary lookup failure")
			}
		}
		return "subscription", true, true, nil
	}
	// Earlier tests may have left events; drain without relying on execution order.
	for i := 0; i < 2000; i++ {
		batch, err := testStore.ProcessSubscriptionBatch(ctx, 1, policy)
		if err != nil {
			if calls == 0 {
				t.Fatal(err)
			}
			break
		}
		if batch.PostID == 0 {
			t.Fatal("event missing")
		}
	}
	failOnce = false
	for i := 0; i < 2000; i++ {
		batch, err := testStore.ProcessSubscriptionBatch(ctx, 1, policy)
		if err != nil {
			t.Fatal(err)
		}
		if batch.PostID == 0 {
			break
		}
	}
	var n int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2 AND type='subscription'`, b, p.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	sub.NotifyEmail = false
	until := time.Now().Add(time.Hour)
	sub.MutedUntil = &until
	if _, err = testStore.SaveSubscription(ctx, b, sub, false); err != nil {
		t.Fatal(err)
	}
	same, err := testStore.SaveSubscription(ctx, b, Subscription{Kind: "forum", TargetID: fid}, true)
	if err != nil || same.NotifyEmail || same.MutedUntil == nil {
		t.Fatal("retry reset preferences", same, err)
	}
	rows, total, err := testStore.Subscriptions(WithVisibleForums(ctx, []int64{}), b, "forum", 1)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal(rows, total, err)
	}
	if err = testStore.DeleteSubscription(ctx, b, fid, "forum"); err != nil {
		t.Fatal(err)
	}
}
