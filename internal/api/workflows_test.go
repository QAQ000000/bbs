// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"dzforum/internal/store"
)

func TestWorkflowApprovedReplyNotification(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	owner, _, _ := memberTestUser(t)
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, owner.ID, owner.Username, "Moderation audit", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, reply, err := smokeSrv.st.CreateReply(ctx, th.ID, 1, "admin", "pending reply without mentions", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/moderate/post", map[string]any{"pid": fmt.Sprint(reply.ID), "op": "approve"}, adminCSRF, adminCookie), 200)
	}
	var count int
	if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2 AND type='reply'`, owner.ID, reply.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("expected exactly one owner notification", count, err)
	}
}

func TestWorkflowDraftsAndOwnModeration(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	body := map[string]any{"context": "new:1", "subject": "saved subject", "content": ""}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/draft", body, "", cookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/draft", body, csrf, cookie), 200)
	for _, path := range []string{"/api/v1/me/draft?context=new:1", "/api/v1/me/drafts"} {
		res := smokeGet(t, path, cookie)
		checkJSON(t, res, 200)
		if !strings.Contains(res.Body.String(), "saved subject") {
			t.Fatal(res.Body.String())
		}
	}
	for _, invalid := range []map[string]any{{"context": "new:foo", "content": "x"}, {"context": "reply:1", "subject": "bad", "content": "x"}, {"context": "new:1", "subject": strings.Repeat("x", 81)}} {
		checkJSON(t, memberJSON(t, "POST", "/api/v1/me/draft", invalid, csrf, cookie), 422)
	}
	if strings.Contains(smokeGet(t, "/api/v1/me/drafts", adminCookie).Body.String(), "saved subject") {
		t.Fatal("draft leaked")
	}
	th, p, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "own pending subject", "private pending body", "", true, "sensitive matcher detail")
	if err != nil {
		t.Fatal(err)
	}
	res := smokeGet(t, "/api/v1/me/content?status=pending", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), "own pending subject") || strings.Contains(res.Body.String(), "sensitive matcher detail") {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/moderate/thread", map[string]any{"tid": fmt.Sprint(th.ID), "op": "delete", "note": "please revise"}, adminCSRF, adminCookie), 200)
	res = smokeGet(t, "/api/v1/me/content?status=rejected", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), "please revise") || !strings.Contains(res.Body.String(), "private pending body") {
		t.Fatal(res.Body.String())
	}
	res = smokeGet(t, "/api/v1/me/notifications", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), "moderation.rejected") {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", p.ID), cookie), 404)
	if strings.Contains(smokeGet(t, "/api/v1/me/content?status=rejected", adminCookie).Body.String(), "private pending body") {
		t.Fatal("another user's rejected body leaked")
	}
	checkJSON(t, smokeGet(t, "/api/v1/me/content", nil), 401)
}

func TestWorkflowNotificationPaginationAndOwnership(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	th, p, err := smokeSrv.st.CreateThread(ctx, 1, 1, "admin", "notification source", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := []*store.Notification{}
	for i := 0; i < 35; i++ {
		rows = append(rows, &store.Notification{UID: u.ID, FromUID: 1, FromName: "admin", Type: "reply", ThreadID: th.ID, PostID: p.ID, Excerpt: fmt.Sprint(i)})
	}
	if err = smokeSrv.st.AddNotifications(ctx, rows); err != nil {
		t.Fatal(err)
	}
	res := smokeGet(t, "/api/v1/me/notifications?page=2&unread=true", cookie)
	data := checkJSON(t, res, 200)
	var list []any
	_ = json.Unmarshal(data["data"], &list)
	if len(list) != 5 || !strings.Contains(res.Body.String(), `"total":35`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/notifications/read", map[string]any{"ids": []string{fmt.Sprint(rows[0].ID)}}, "", cookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/notifications/read", map[string]any{"ids": []string{fmt.Sprint(rows[0].ID)}}, adminCSRF, adminCookie), 200)
	res = smokeGet(t, "/api/v1/me/notifications/summary", cookie)
	if !strings.Contains(res.Body.String(), `"unread":35`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/notifications/read", map[string]any{"ids": []string{fmt.Sprint(rows[0].ID)}}, csrf, cookie), 200)
	res = smokeGet(t, "/api/v1/me/notifications/summary", cookie)
	if !strings.Contains(res.Body.String(), `"unread":34`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/notifications/read", map[string]any{}, csrf, cookie), 422)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/notifications/read", map[string]any{"all": true}, csrf, cookie), 200)
	prefs := map[string]bool{}
	for _, key := range store.NotificationPreferenceKeys {
		prefs[key] = true
	}
	prefs["replies"] = false
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/notification-preferences", prefs, "", cookie), 403)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/notification-preferences", prefs, csrf, cookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/me/notification-preferences", cookie), 200)
	if err = smokeSrv.st.AddNotifications(ctx, rows[:1]); err != nil || rows[0].ID != 0 {
		t.Fatal("disabled reply inserted", err)
	}
	res = smokeGet(t, "/api/v1/me/notifications/summary", cookie)
	if !strings.Contains(res.Body.String(), `"unread":0`) {
		t.Fatal(res.Body.String())
	}
}

func TestWorkflowReplyRelationAndVisibility(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, 1, "admin", "reply relation", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, target, err := smokeSrv.st.CreateReply(ctx, th.ID, 2, "member", "target", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/threads/%d/posts", th.ID)
	data := checkJSON(t, memberJSON(t, "POST", path, map[string]any{"content": "direct reply content", "replyToPostId": fmt.Sprint(target.ID)}, csrf, cookie), 201)
	var created struct {
		PostID string `json:"postId"`
	}
	_ = json.Unmarshal(data["data"], &created)
	res := smokeGet(t, "/api/v1/posts/"+created.PostID, cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"replyTo":{"id":"`+fmt.Sprint(target.ID)+`","available":true`) {
		t.Fatal(res.Body.String())
	}
	var count int
	if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE post_id=$1 AND type IN ('reply','reply.direct')`, created.PostID).Scan(&count); err != nil || count != 2 {
		t.Fatal("missing direct or owner notification", count, err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/posts/"+created.PostID+"/position", cookie), 200)
	if _, _, err = smokeSrv.st.LikeToggle(ctx, target.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	res = smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", target.ID), cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"viewerHasLiked":true`) {
		t.Fatal(res.Body.String())
	}
	if err = smokeSrv.st.SetPostPendingModeration(ctx, target.ID, "hidden"); err != nil {
		t.Fatal(err)
	}
	res = smokeGet(t, "/api/v1/posts/"+created.PostID, cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"available":false`) || strings.Contains(res.Body.String(), `"authorName":"user01"`) {
		t.Fatal("target privacy", res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"content": "blocked target", "replyToPostId": fmt.Sprint(target.ID)}, csrf, cookie), 404)
	_, other, err := smokeSrv.st.CreateThread(ctx, 1, 1, "admin", "other", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"content": "cross thread", "replyToPostId": fmt.Sprint(other.ID)}, csrf, cookie), 404)
}

func TestWorkflowRestrictedNotificationCounts(t *testing.T) {
	c := memberAPIConfig(t)
	ctx := context.Background()
	u, cookie, _ := memberTestUser(t)
	th, p, err := smokeSrv.st.CreateThread(ctx, smokeFid2, u.ID, u.Username, "private result", "body", "", true, "review")
	if err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.AddNotifications(ctx, []*store.Notification{{UID: u.ID, FromUID: 1, Type: "reply", ThreadID: th.ID, PostID: p.ID, Excerpt: "private excerpt"}}); err != nil {
		t.Fatal(err)
	}
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: smokeFid2, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, c)
	res := smokeGet(t, "/api/v1/me/notifications", cookie)
	checkJSON(t, res, 200)
	if strings.Contains(res.Body.String(), "private excerpt") || !strings.Contains(res.Body.String(), "moderation.approved") {
		t.Fatal(res.Body.String())
	}
	res = smokeGet(t, "/api/v1/me/notifications/summary", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"unread":1`) {
		t.Fatal(res.Body.String())
	}
	res = smokeGet(t, "/api/v1/me/content", cookie)
	checkJSON(t, res, 200)
	if strings.Contains(res.Body.String(), "private result") {
		t.Fatal("restricted own content leaked")
	}
}
