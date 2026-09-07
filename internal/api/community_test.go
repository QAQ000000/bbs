// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"dzforum/internal/store"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMessagingAPIPrivacyReadAndBlock(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	for _, path := range []string{"/api/v1/me/conversations", "/api/v1/conversations/1/messages", "/api/v1/me/following", "/api/v1/me/subscriptions"} {
		checkJSON(t, smokeGet(t, path, nil), 401)
	}
	route := fmt.Sprintf("/api/v1/users/%d/messages", u.ID)
	checkJSON(t, memberJSON(t, "POST", route, map[string]any{"body": "hello"}, "", adminCookie), 403)
	data := checkJSON(t, memberJSON(t, "POST", route, map[string]any{"body": "hello private"}, adminCSRF, adminCookie), 201)
	var msg store.Message
	if err := json.Unmarshal(data["data"], &msg); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "POST", route, map[string]any{"body": "spam"}, adminCSRF, adminCookie), 409)
	path := fmt.Sprintf("/api/v1/conversations/%d", msg.ConversationID)
	checkJSON(t, smokeGet(t, path+"/messages", userCookie), 404)
	res := smokeGet(t, "/api/v1/me/conversations", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"unread":1`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", path+"/read", map[string]any{"messageId": fmt.Sprint(msg.ID)}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "POST", path+"/block", map[string]any{}, userCSRF, userCookie), 404)
	checkJSON(t, memberJSON(t, "POST", path+"/block", map[string]any{}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "POST", route, map[string]any{"body": "blocked"}, adminCSRF, adminCookie), 403)
	checkJSON(t, memberJSON(t, "DELETE", path+"/block", map[string]any{}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/users/1/messages", map[string]any{"body": "reply"}, csrf, cookie), 201)
	checkJSON(t, memberJSON(t, "POST", route, map[string]any{"body": "unrestricted"}, adminCSRF, adminCookie), 201)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/users/999999/messages", map[string]any{"body": "invalid"}, csrf, cookie), 404)
	rows, _, err := smokeSrv.st.Conversations(ctx, u.ID, 1)
	if err != nil || len(rows) != 1 || rows[0].Unread != 1 {
		t.Fatal(rows, err)
	}
}

func TestTagsAndSubscriptionAPI(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	def := map[string]any{"name": "api-tag", "slug": "api-tag", "color": "#00aa33"}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/tags", def, csrf, cookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/tags", def, "", adminCookie), 403)
	data := checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/tags", def, adminCSRF, adminCookie), 201)
	var tag store.Tag
	if err := json.Unmarshal(data["data"], &tag); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/tag-slugs/api-tag", nil), 200)
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/tags/%d/subscribe", tag.ID), map[string]any{}, csrf, cookie), 200)
	data = checkJSON(t, memberJSON(t, "POST", "/api/v1/threads", map[string]any{"forumId": "1", "subject": "tag API topic", "content": "body", "tagIds": []string{fmt.Sprint(tag.ID)}}, adminCSRF, adminCookie), 201)
	var created struct {
		ThreadID string `json:"threadId"`
		PostID   string `json:"postId"`
	}
	_ = json.Unmarshal(data["data"], &created)
	res := smokeGet(t, "/api/v1/threads/"+created.ThreadID, nil)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"slug":"api-tag"`) {
		t.Fatal(res.Body.String())
	}
	for i := 0; i < 2000; i++ {
		n, err := smokeSrv.ProcessSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var n int
	if err := smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2 AND type='subscription'`, u.ID, created.PostID).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/threads/"+created.ThreadID+"/tags", map[string]any{"version": 1, "tagIds": []string{}}, csrf, cookie), 403)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/threads/"+created.ThreadID+"/tags", map[string]any{"version": 1, "tagIds": []string{}}, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/threads/"+created.ThreadID+"/tags", map[string]any{"version": 1, "tagIds": []string{fmt.Sprint(tag.ID)}}, adminCSRF, adminCookie), 409)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/tags/%d/threads", tag.ID), cookie), 200)
	subPath := "/api/v1/threads/" + created.ThreadID + "/subscribe"
	checkJSON(t, memberJSON(t, "POST", subPath, map[string]any{}, csrf, cookie), 200)
	prefs := map[string]any{"enabled": true, "notifyInApp": true, "notifyEmail": false}
	checkJSON(t, memberJSON(t, "PUT", subPath, prefs, csrf, cookie), 200)
	res = memberJSON(t, "POST", subPath, map[string]any{}, csrf, cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"notifyEmail":false`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, smokeGet(t, "/api/v1/me/subscriptions", cookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/users/1/follow", map[string]any{}, csrf, cookie), 200)
	res = smokeGet(t, "/api/v1/me/following", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"username":"admin"`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/users/999999/follow", map[string]any{}, csrf, cookie), 404)
}

func TestSubscriptionPermissionsAtDelivery(t *testing.T) {
	c := memberAPIConfig(t)
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	_, err := smokeSrv.st.SaveSubscription(ctx, u.ID, store.Subscription{Kind: "forum", TargetID: smokeFid2}, true)
	if err != nil {
		t.Fatal(err)
	}
	th, p, err := smokeSrv.st.CreateThread(ctx, smokeFid2, 1, "admin", "restricted subscription", "body", "", true, "review")
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/threads/%d/subscribe", th.ID), map[string]any{}, csrf, cookie), 404)
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: smokeFid2, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, c)
	if err = smokeSrv.st.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		n, err := smokeSrv.ProcessSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var n int
	if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2`, u.ID, p.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("restricted notification delivered", n, err)
	}
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/forums/%d/subscribe", smokeFid2), map[string]any{}, csrf, cookie), 404)
	checkJSON(t, memberJSON(t, "DELETE", fmt.Sprintf("/api/v1/forums/%d/subscribe", smokeFid2), map[string]any{}, csrf, cookie), 200)
}
