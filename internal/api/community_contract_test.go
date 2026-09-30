package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dzforum/internal/store"
)

func TestOpenAPICommunityTags(t *testing.T) {
	u, cookie, csrf := memberTestUser(t)
	doc, ctx := loadAPIContract(t), context.Background()
	definition := map[string]any{"name": "  Contract   Tag ", "slug": "CONTRACT-TAG", "color": "#12ab34", "description": "original"}
	assertContractResponse(t, doc, "POST", "/api/v1/admin/tags", memberJSON(t, "POST", "/api/v1/admin/tags", definition, csrf, cookie), 403)
	assertContractResponse(t, doc, "POST", "/api/v1/admin/tags", memberJSON(t, "POST", "/api/v1/admin/tags", definition, "", adminCookie), 403)
	tag := assertContractResponse(t, doc, "POST", "/api/v1/admin/tags", memberJSON(t, "POST", "/api/v1/admin/tags", definition, adminCSRF, adminCookie), 201)["data"].(contractObject)
	if tag["name"] != "contract tag" || tag["slug"] != "contract-tag" || tag["status"] != "active" {
		t.Fatal("tag normalization/default changed", tag)
	}
	id := tag["id"].(string)
	tagPath, adminPath := "/api/v1/tags/"+id, "/api/v1/admin/tags/"+id
	update := map[string]any{"name": "contract renamed", "slug": "contract-renamed", "status": "active", "version": tag["version"]}
	tag = assertContractResponse(t, doc, "PUT", "/api/v1/admin/tags/{tagId}", memberJSON(t, "PUT", adminPath, update, adminCSRF, adminCookie), 200)["data"].(contractObject)
	if tag["color"] != "" || tag["description"] != "" {
		t.Fatal("full replacement must clear omitted fields", tag)
	}
	assertContractResponse(t, doc, "PUT", "/api/v1/admin/tags/{tagId}", memberJSON(t, "PUT", adminPath, update, adminCSRF, adminCookie), 409)
	definition["name"] = "another tag"
	assertContractResponse(t, doc, "POST", "/api/v1/admin/tags", memberJSON(t, "POST", "/api/v1/admin/tags", definition, adminCSRF, adminCookie), 409)
	alias := assertContractResponse(t, doc, "GET", "/api/v1/tag-slugs/{slug}", smokeGet(t, "/api/v1/tag-slugs/CONTRACT-TAG", nil), 200)["data"].(contractObject)
	if alias["id"] != id || alias["slug"] != "contract-renamed" {
		t.Fatal(alias)
	}
	th, post, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "community tags contract", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	tagsPath := fmt.Sprintf("/api/v1/threads/%d/tags", th.ID)
	binding := map[string]any{"version": post.Version, "tagIds": []string{id}}
	assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, userCSRF, userCookie), 403)
	saved := assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, csrf, cookie), 200)["data"].(contractObject)
	assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, csrf, cookie), 409)
	binding["version"], binding["tagIds"] = saved["version"], []string{id, id}
	assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, csrf, cookie), 422)
	topics := assertContractResponse(t, doc, "GET", "/api/v1/tags/{tagId}/threads", smokeGet(t, tagPath+"/threads", nil), 200)["data"].([]any)
	if len(topics) != 1 || topics[0].(contractObject)["id"] != idString(th.ID) || len(topics[0].(contractObject)["tags"].([]any)) != 1 {
		t.Fatal(topics)
	}
	listed := assertContractResponse(t, doc, "GET", "/api/v1/tags", smokeGet(t, "/api/v1/tags?q=contract-renamed", nil), 200)["data"].([]any)
	if len(listed) != 1 || listed[0].(contractObject)["threadCount"] != float64(1) {
		t.Fatal(listed)
	}
	detail := assertContractResponse(t, doc, "GET", "/api/v1/tags/{tagId}", smokeGet(t, tagPath, nil), 200)["data"].(contractObject)
	if detail["threadCount"] != float64(0) {
		t.Fatal("detail count is an uncomputed projection", detail)
	}
	update["version"], update["status"] = tag["version"], "disabled"
	assertContractResponse(t, doc, "PUT", "/api/v1/admin/tags/{tagId}", memberJSON(t, "PUT", adminPath, update, adminCSRF, adminCookie), 200)
	binding["tagIds"] = []string{id}
	saved = assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, csrf, cookie), 200)["data"].(contractObject)
	binding["version"], binding["tagIds"] = saved["version"], []string{}
	saved = assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, csrf, cookie), 200)["data"].(contractObject)
	binding["version"], binding["tagIds"] = saved["version"], []string{id}
	assertContractResponse(t, doc, "PUT", "/api/v1/threads/{tid}/tags", memberJSON(t, "PUT", tagsPath, binding, csrf, cookie), 422)
	listed = assertContractResponse(t, doc, "GET", "/api/v1/tags", smokeGet(t, "/api/v1/tags?q=contract-renamed", nil), 200)["data"].([]any)
	if len(listed) != 0 {
		t.Fatal("disabled tag in public directory", listed)
	}
	listed = assertContractResponse(t, doc, "GET", "/api/v1/admin/tags", smokeGet(t, "/api/v1/admin/tags?q=contract-renamed", adminCookie), 200)["data"].([]any)
	if len(listed) != 1 || listed[0].(contractObject)["status"] != "disabled" {
		t.Fatal(listed)
	}
	assertContractResponse(t, doc, "GET", "/api/v1/admin/tags", smokeGet(t, "/api/v1/admin/tags", cookie), 403)
	assertContractResponse(t, doc, "GET", "/api/v1/tags/{tagId}", smokeGet(t, tagPath, nil), 200)
}

func TestOpenAPICommunityRelations(t *testing.T) {
	u, cookie, csrf := memberTestUser(t)
	doc, ctx := loadAPIContract(t), context.Background()
	for _, method := range []string{"POST", "POST"} {
		data := assertContractResponse(t, doc, method, "/api/v1/users/{id}/follow", memberJSON(t, method, "/api/v1/users/1/follow", map[string]any{}, csrf, cookie), 200)["data"].(contractObject)
		if data["following"] != true || data["userId"] != "1" {
			t.Fatal(data)
		}
	}
	following := assertContractResponse(t, doc, "GET", "/api/v1/me/following", smokeGet(t, "/api/v1/me/following", cookie), 200)["data"].([]any)
	if len(following) != 1 || following[0].(contractObject)["id"] != "1" {
		t.Fatal(following)
	}
	for _, tc := range []struct {
		route, path string
		cookie      *http.Cookie
	}{
		{"/api/v1/me/followers", "/api/v1/me/followers", adminCookie},
		{"/api/v1/users/{id}/followers", "/api/v1/users/1/followers", cookie},
	} {
		rows := assertContractResponse(t, doc, "GET", tc.route, smokeGet(t, tc.path, tc.cookie), 200)["data"].([]any)
		found := false
		for _, row := range rows {
			found = found || row.(contractObject)["id"] == idString(u.ID)
		}
		if !found {
			t.Fatal("follower missing", rows)
		}
	}
	assertContractResponse(t, doc, "GET", "/api/v1/users/{id}/followers", smokeGet(t, "/api/v1/users/1/followers", nil), 401)
	assertContractResponse(t, doc, "POST", "/api/v1/users/{id}/follow", memberJSON(t, "POST", fmt.Sprintf("/api/v1/users/%d/follow", u.ID), map[string]any{}, csrf, cookie), 422)
	for i := 0; i < 2; i++ {
		assertContractResponse(t, doc, "DELETE", "/api/v1/users/{id}/follow", memberJSON(t, "DELETE", "/api/v1/users/1/follow", map[string]any{}, csrf, cookie), 200)
	}
	tag, err := smokeSrv.st.SaveTag(ctx, store.Tag{Name: "contract-subscription", Slug: "contract-subscription", Status: "active"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ kind, route, path string }{
		{"thread", "/api/v1/threads/{tid}/subscribe", "/api/v1/threads/1/subscribe"},
		{"forum", "/api/v1/forums/{fid}/subscribe", "/api/v1/forums/1/subscribe"},
		{"tag", "/api/v1/tags/{tagId}/subscribe", fmt.Sprintf("/api/v1/tags/%d/subscribe", tag.ID)},
	}
	assertContractResponse(t, doc, "GET", "/api/v1/me/subscriptions", smokeGet(t, "/api/v1/me/subscriptions", nil), 401)
	assertContractResponse(t, doc, "POST", cases[0].route, memberJSON(t, "POST", cases[0].path, map[string]any{}, "", cookie), 403)
	for _, tc := range cases {
		data := assertContractResponse(t, doc, "POST", tc.route, memberJSON(t, "POST", tc.path, map[string]any{"notifyEmail": false}, csrf, cookie), 200)["data"].(contractObject)
		if data["kind"] != tc.kind || data["enabled"] != true || data["notifyInApp"] != true || data["notifyEmail"] != true || data["mutedUntil"] != nil {
			t.Fatal("new subscription defaults", data)
		}
		originalTime := data["createdAt"]
		prefs := map[string]any{"enabled": true, "notifyInApp": false, "notifyEmail": false, "mutedUntil": "2030-01-02T03:04:05Z"}
		assertContractResponse(t, doc, "PUT", tc.route, memberJSON(t, "PUT", tc.path, prefs, csrf, cookie), 200)
		data = assertContractResponse(t, doc, "POST", tc.route, memberJSON(t, "POST", tc.path, map[string]any{}, csrf, cookie), 200)["data"].(contractObject)
		if data["notifyEmail"] != false || data["notifyInApp"] != false || data["mutedUntil"] == nil || data["createdAt"] != originalTime {
			t.Fatal("POST changed existing preferences", data)
		}
		delete(prefs, "mutedUntil")
		data = assertContractResponse(t, doc, "PUT", tc.route, memberJSON(t, "PUT", tc.path, prefs, csrf, cookie), 200)["data"].(contractObject)
		if data["mutedUntil"] != nil {
			t.Fatal("omitted mute should clear", data)
		}
		rows := assertContractResponse(t, doc, "GET", "/api/v1/me/subscriptions", smokeGet(t, "/api/v1/me/subscriptions?kind="+tc.kind, cookie), 200)["data"].([]any)
		if len(rows) != 1 || rows[0].(contractObject)["name"] == nil {
			t.Fatal("list projection missing name", rows)
		}
		delete(prefs, "notifyEmail")
		assertContractResponse(t, doc, "PUT", tc.route, memberJSON(t, "PUT", tc.path, prefs, csrf, cookie), 422)
	}
	if _, err := smokePool.Exec(ctx, "UPDATE users SET must_change_password=true WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		for _, method := range []string{"POST", "PUT"} {
			result := assertContractResponse(t, doc, method, tc.route, memberJSON(t, method, tc.path, map[string]any{"enabled": true, "notifyInApp": true, "notifyEmail": true}, csrf, cookie), 403)
			if !strings.Contains(result["error"].(contractObject)["message"].(string), "初始密码") {
				t.Fatal("wrong guard", result)
			}
		}
		assertContractResponse(t, doc, "DELETE", tc.route, memberJSON(t, "DELETE", tc.path, map[string]any{}, csrf, cookie), 200)
		assertContractResponse(t, doc, "DELETE", tc.route, memberJSON(t, "DELETE", tc.path, map[string]any{}, csrf, cookie), 200)
		rows := assertContractResponse(t, doc, "GET", "/api/v1/me/subscriptions", smokeGet(t, "/api/v1/me/subscriptions?kind="+tc.kind, cookie), 200)["data"].([]any)
		if len(rows) != 0 {
			t.Fatal("cancellation blocked by password guard", rows)
		}
	}
	if _, err := smokePool.Exec(ctx, "UPDATE users SET must_change_password=false WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	tag.Status = "disabled"
	if _, err := smokeSrv.st.SaveTag(ctx, tag, 1); err != nil {
		t.Fatal(err)
	}
	assertContractResponse(t, doc, "POST", cases[2].route, memberJSON(t, "POST", cases[2].path, map[string]any{}, csrf, cookie), 422)
	assertContractResponse(t, doc, "GET", "/api/v1/me/subscriptions", smokeGet(t, "/api/v1/me/subscriptions?kind=unknown", cookie), 422)
	prefPath := "/api/v1/me/notification-preferences"
	prefs := assertContractResponse(t, doc, "GET", prefPath, smokeGet(t, prefPath, cookie), 200)["data"].(contractObject)
	for _, key := range store.NotificationPreferenceKeys {
		if prefs[key] != true {
			t.Fatal("default preference", key, prefs)
		}
	}
	prefs["subscriptions"], prefs["email"] = false, false
	assertContractResponse(t, doc, "PUT", prefPath, memberJSON(t, "PUT", prefPath, prefs, csrf, cookie), 200)
	delete(prefs, "subscriptions")
	saved := assertContractResponse(t, doc, "PUT", prefPath, memberJSON(t, "PUT", prefPath, prefs, csrf, cookie), 200)["data"].(contractObject)
	if saved["subscriptions"] != false {
		t.Fatal("legacy save reset subscriptions", saved)
	}
	delete(prefs, "email")
	assertContractResponse(t, doc, "PUT", prefPath, memberJSON(t, "PUT", prefPath, prefs, csrf, cookie), 422)
}

func TestOpenAPICommunityMessaging(t *testing.T) {
	u, cookie, csrf := memberTestUser(t)
	ctx, doc := context.Background(), loadAPIContract(t)
	recipient, err := smokeSrv.st.CreateUser(ctx, "recipient_"+t.Name(), "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	token, recipientCSRF, err := smokeSrv.st.CreateSession(ctx, recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipientCookie := &http.Cookie{Name: cookieSession, Value: token}
	sendPath, replyPath := fmt.Sprintf("/api/v1/users/%d/messages", recipient.ID), fmt.Sprintf("/api/v1/users/%d/messages", u.ID)
	send := func(path, body, csrf string, cookie *http.Cookie, status int) contractObject {
		t.Helper()
		return assertContractResponse(t, doc, "POST", "/api/v1/users/{id}/messages", memberJSON(t, "POST", path, map[string]any{"body": body}, csrf, cookie), status)
	}
	send(sendPath, "hello", "", cookie, 403)
	first := send(sendPath, "hello", csrf, cookie, 201)["data"].(contractObject)
	cid, firstID := first["conversationId"].(string), first["id"].(string)
	base := "/api/v1/conversations/" + cid
	denied := send(sendPath, "must wait", csrf, cookie, 409)
	if denied["error"].(contractObject)["code"] != "MESSAGE_REPLY_REQUIRED" {
		t.Fatal(denied)
	}
	conversation := func(cookie *http.Cookie) contractObject {
		t.Helper()
		rows := assertContractResponse(t, doc, "GET", "/api/v1/me/conversations", smokeGet(t, "/api/v1/me/conversations", cookie), 200)["data"].([]any)
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		return rows[0].(contractObject)
	}
	if conversation(cookie)["waitingForReply"] != true || conversation(recipientCookie)["waitingForReply"] != false || conversation(recipientCookie)["unread"] != float64(1) {
		t.Fatal("initial conversation state")
	}
	assertContractResponse(t, doc, "GET", "/api/v1/conversations/{cid}/messages", smokeGet(t, base+"/messages", adminCookie), 404)
	assertContractResponse(t, doc, "POST", "/api/v1/conversations/{cid}/read", memberJSON(t, "POST", base+"/read", map[string]any{"messageId": firstID}, adminCSRF, adminCookie), 404)
	assertContractResponse(t, doc, "POST", "/api/v1/conversations/{cid}/block", memberJSON(t, "POST", base+"/block", map[string]any{}, adminCSRF, adminCookie), 404)
	block := func(method, csrf string, cookie *http.Cookie) {
		t.Helper()
		assertContractResponse(t, doc, method, "/api/v1/conversations/{cid}/block", memberJSON(t, method, base+"/block", map[string]any{}, csrf, cookie), 200)
	}
	block("POST", recipientCSRF, recipientCookie)
	block("DELETE", recipientCSRF, recipientCookie)
	send(sendPath, "still wait", csrf, cookie, 409)
	reply := send(replyPath, "reply", recipientCSRF, recipientCookie, 201)["data"].(contractObject)
	third := send(sendPath, "unlocked", csrf, cookie, 201)["data"].(contractObject)
	if conversation(cookie)["waitingForReply"] != false {
		t.Fatal("reply must unlock sender")
	}
	rows := assertContractResponse(t, doc, "GET", "/api/v1/conversations/{cid}/messages", smokeGet(t, base+"/messages?before="+third["id"].(string), cookie), 200)["data"].([]any)
	if len(rows) != 2 || rows[0].(contractObject)["id"] != reply["id"] || rows[1].(contractObject)["id"] != firstID {
		t.Fatal("exclusive descending cursor", rows)
	}
	rows = assertContractResponse(t, doc, "GET", "/api/v1/conversations/{cid}/messages", smokeGet(t, base+"/messages?before="+firstID, cookie), 200)["data"].([]any)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
	assertContractResponse(t, doc, "GET", "/api/v1/conversations/{cid}/messages", smokeGet(t, base+"/messages?before=bad", cookie), 422)
	if conversation(cookie)["unread"] != float64(1) {
		t.Fatal("history read must not mark read")
	}
	for _, mid := range []any{third["id"], firstID} {
		assertContractResponse(t, doc, "POST", "/api/v1/conversations/{cid}/read", memberJSON(t, "POST", base+"/read", map[string]any{"messageId": mid}, csrf, cookie), 200)
	}
	if conversation(cookie)["unread"] != float64(0) {
		t.Fatal("old cursor moved read progress backwards")
	}
	assertContractResponse(t, doc, "POST", "/api/v1/conversations/{cid}/read", memberJSON(t, "POST", base+"/read", map[string]any{"messageId": "999999999"}, csrf, cookie), 404)
	for _, tc := range []struct {
		id     int64
		csrf   string
		cookie *http.Cookie
	}{{recipient.ID, csrf, cookie}, {u.ID, recipientCSRF, recipientCookie}} {
		assertContractResponse(t, doc, "POST", "/api/v1/users/{id}/follow", memberJSON(t, "POST", fmt.Sprintf("/api/v1/users/%d/follow", tc.id), map[string]any{}, tc.csrf, tc.cookie), 200)
	}
	block("POST", csrf, cookie)
	block("POST", csrf, cookie)
	if conversation(cookie)["blocked"] != true || conversation(recipientCookie)["blocked"] != false {
		t.Fatal("blocked field leaks or conflates peer state")
	}
	denied = send(sendPath, "own block", csrf, cookie, 403)
	if denied["error"].(contractObject)["code"] != "MESSAGE_BLOCKED" || strings.Contains(denied["error"].(contractObject)["message"].(string), "对方已屏蔽你") {
		t.Fatal(denied)
	}
	send(replyPath, "peer block", recipientCSRF, recipientCookie, 403)
	assertContractResponse(t, doc, "POST", "/api/v1/users/{id}/follow", memberJSON(t, "POST", fmt.Sprintf("/api/v1/users/%d/follow", u.ID), map[string]any{}, recipientCSRF, recipientCookie), 403)
	block("POST", recipientCSRF, recipientCookie)
	block("DELETE", csrf, cookie)
	send(sendPath, "peer still blocked", csrf, cookie, 403)
	block("DELETE", recipientCSRF, recipientCookie)
	send(sendPath, "unblocked", csrf, cookie, 201)
	for _, c := range []*http.Cookie{cookie, recipientCookie} {
		rows := assertContractResponse(t, doc, "GET", "/api/v1/me/following", smokeGet(t, "/api/v1/me/following", c), 200)["data"].([]any)
		if len(rows) != 0 {
			t.Fatal("unblock must not restore following", rows)
		}
	}
}
