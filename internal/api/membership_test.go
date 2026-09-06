// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func memberAPIConfig(t *testing.T) store.MembershipConfig {
	t.Helper()
	requireDB(t)
	c, err := smokeSrv.st.MembershipConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	t.Cleanup(func() {
		if _, err := smokePool.Exec(context.Background(), `UPDATE membership_config SET version=$1,body=$2 WHERE id`, c.Version, b); err != nil {
			t.Error(err)
		}
	})
	return c
}
func setMemberAPIConfig(t *testing.T, c store.MembershipConfig) {
	t.Helper()
	b, _ := json.Marshal(c)
	if _, err := smokePool.Exec(context.Background(), `UPDATE membership_config SET version=$1,body=$2 WHERE id`, c.Version, b); err != nil {
		t.Fatal(err)
	}
}
func memberJSON(t *testing.T, method, path string, v any, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(v)
	return apiRequest(t, method, path, csrf, string(b), cookie)
}
func memberTestUser(t *testing.T) (*store.User, *http.Cookie, string) {
	t.Helper()
	requireDB(t)
	u, err := smokeSrv.st.CreateUser(context.Background(), "member_"+t.Name(), "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	token, csrf, err := smokeSrv.st.CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u, &http.Cookie{Name: cookieSession, Value: token}, csrf
}

func TestMemberConfigurationPreviewAndCSRF(t *testing.T) {
	c := memberAPIConfig(t)
	c.Levels[0].Name = "见习会员"
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/membership/preview", c, "", adminCookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/membership/preview", c, userCSRF, userCookie), 403)
	data := checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/membership/preview", c, adminCSRF, adminCookie), 200)
	var preview store.MemberPreview
	if err := json.Unmarshal(data["data"], &preview); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/admin/membership", map[string]any{"config": c, "previewToken": "bad"}, adminCSRF, adminCookie), 409)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/admin/membership", map[string]any{"config": c, "previewToken": preview.Token}, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/admin/membership", map[string]any{"config": c, "previewToken": preview.Token}, adminCSRF, adminCookie), 409)
	res := smokeGet(t, "/api/v1/membership/levels", nil)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), "见习会员") {
		t.Fatal("new configuration not visible")
	}
	c.Version++
	c.Levels[0].Permissions["admin.panel"] = true
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/membership/preview", c, adminCSRF, adminCookie), 422)
}
func TestMemberFinePermissionsAndQuotaRefund(t *testing.T) {
	c := memberAPIConfig(t)
	u, cookie, csrf := memberTestUser(t)
	c.Levels[0].Permissions["thread.create"] = false
	setMemberAPIConfig(t, c)
	body := map[string]any{"forumId": "1", "subject": "member controlled thread", "content": "plain content"}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/threads", body, csrf, cookie), 403)
	c.Levels[0].Permissions["thread.create"] = true
	c.Levels[0].Limits.ThreadsPerDay = 1
	setMemberAPIConfig(t, c)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/threads", map[string]any{"forumId": "1", "subject": "", "content": "invalid"}, csrf, cookie), 422)
	usage, err := smokeSrv.st.MemberQuota(context.Background(), u.ID)
	if err != nil || usage["thread.create"] != 0 {
		t.Fatal("failed content consumed quota", err, usage)
	}
	data := checkJSON(t, memberJSON(t, "POST", "/api/v1/threads", body, csrf, cookie), 201)
	var created struct {
		PostID   string `json:"postId"`
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(data["data"], &created)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/threads", body, csrf, cookie), 429)
	c.Levels[0].Permissions["post.reply"] = false
	c.Levels[0].Limits.EditMinutes = 0
	setMemberAPIConfig(t, c)
	res := smokeGet(t, "/api/v1/threads/"+created.ThreadID, cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"canReply":false`) {
		t.Fatal("reply capability ignores level")
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/threads/"+created.ThreadID+"/posts", map[string]any{"content": "reply"}, csrf, cookie), 403)
	res = smokeGet(t, "/api/v1/posts/"+created.PostID, cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"canEdit":false`) {
		t.Fatal("edit capability ignores time limit")
	}
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/posts/"+created.PostID, map[string]any{"subject": "edited", "content": "edited", "version": 1}, csrf, cookie), 403)
}
func TestMemberRestrictedForumAllReadSurfaces(t *testing.T) {
	c := memberAPIConfig(t)
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	fid, err := smokeSrv.st.SaveForum(ctx, 0, 1, "会员限制版块", "private", "mod01")
	if err != nil {
		t.Fatal(err)
	}
	th, p, err := smokeSrv.st.CreateThread(ctx, fid, u.ID, u.Username, "restrictedmembersecret", "restrictedmembersecret", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := smokeSrv.st.FavoriteToggle(ctx, u.ID, th.ID); err != nil {
		t.Fatal(err)
	}
	if err := smokeSrv.st.AddNotifications(ctx, []*store.Notification{{UID: u.ID, FromUID: 1, FromName: "admin", Type: "reply", ThreadID: th.ID, PostID: p.ID, Excerpt: "restrictedmembersecret"}}); err != nil {
		t.Fatal(err)
	}
	name := "/uploads/2026/09/abc123abc123abcd.txt"
	disk := filepath.Join(smokeSrv.cfg.UploadDir, strings.TrimPrefix(name, "/uploads/"))
	if err := os.MkdirAll(filepath.Dir(disk), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, []byte("restrictedmembersecret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := smokeSrv.st.SaveUpload(ctx, u.ID, "private.txt", name, 22, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := smokeSrv.st.LinkUploadsToPost(ctx, u.ID, p.ID, []string{name}); err != nil {
		t.Fatal(err)
	}
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: fid, MinimumLevel: 2, MembersOnly: true})
	setMemberAPIConfig(t, c)
	for _, path := range []string{fmt.Sprintf("/api/v1/forums/%d", fid), fmt.Sprintf("/api/v1/threads/%d", th.ID), fmt.Sprintf("/api/v1/threads/%d/posts", th.ID), fmt.Sprintf("/api/v1/posts/%d", p.ID), name} {
		for _, viewer := range []*http.Cookie{nil, cookie} {
			res := smokeGet(t, path, viewer)
			if res.Code != 404 {
				t.Fatalf("%s returned %d: %s", path, res.Code, res.Body.String())
			}
		}
	}
	for _, path := range []string{"/api/v1/home", "/api/v1/forums", "/api/v1/threads", "/api/v1/search?q=restrictedmembersecret", fmt.Sprintf("/api/v1/users/%d", u.ID), fmt.Sprintf("/api/v1/users/%d?tab=replies", u.ID), "/api/v1/me/favorites", "/api/v1/me/notifications"} {
		res := smokeGet(t, path, cookie)
		checkJSON(t, res, 200)
		if strings.Contains(res.Body.String(), "restrictedmembersecret") {
			t.Fatalf("restricted content leaked via %s", path)
		}
	}
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/events?forums=%d", fid), cookie), 403)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/events?thread=%d", th.ID), cookie), 403)
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/threads/%d/posts", th.ID), map[string]any{"content": "blocked"}, csrf, cookie), 404)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/threads/%d", th.ID), modCookie), 200)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/threads/%d", th.ID), adminCookie), 200)
	// Even ownership and a forged crawler UA must not bypass membership access.
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/threads/%d", th.ID), nil)
	r.AddCookie(cookie)
	r.Header.Set("User-Agent", "GPTBot")
	w := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(w, r)
	checkJSON(t, w, 404)
}
func TestMemberUploadLimitsAndBadges(t *testing.T) {
	c := memberAPIConfig(t)
	_, cookie, csrf := memberTestUser(t)
	c.Levels[0].Permissions["upload.image"] = false
	setMemberAPIConfig(t, c)
	checkJSON(t, smokeMultipart(t, "/api/v1/uploads", csrf, "file", "a.png", pngMagic, nil, cookie), 403)
	c.Levels[0].Permissions["upload.image"] = true
	c.Levels[0].Limits.ImageBytes = 2
	setMemberAPIConfig(t, c)
	checkJSON(t, smokeMultipart(t, "/api/v1/uploads", csrf, "file", "a.png", pngMagic, nil, cookie), 413)
	c.Levels[0].Limits.ImageBytes = 1000
	c.Levels[0].Limits.UploadBytesPerDay = 2
	setMemberAPIConfig(t, c)
	checkJSON(t, smokeMultipart(t, "/api/v1/uploads", csrf, "file", "a.png", pngMagic, nil, cookie), 429)
	c.Levels[0].Limits.UploadBytesPerDay = 1000
	c.Levels[0].Limits.UploadsPerDay = 1
	setMemberAPIConfig(t, c)
	checkJSON(t, smokeMultipart(t, "/api/v1/uploads", csrf, "file", "a.png", pngMagic, nil, cookie), 201)
	checkJSON(t, smokeMultipart(t, "/api/v1/uploads", csrf, "file", "a.png", pngMagic, nil, cookie), 429)
	for _, path := range []string{"/api/v1/session", "/api/v1/me", "/api/v1/me/membership"} {
		res := smokeGet(t, path, cookie)
		checkJSON(t, res, 200)
		if !strings.Contains(res.Body.String(), `"badge"`) {
			t.Fatal("user badge missing", path)
		}
	}
	res := smokeGet(t, "/api/v1/threads/1/posts", nil)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"authorLevel"`) {
		t.Fatal("post author level missing")
	}
}
func TestMemberAdminPermissionRevocationAndDiagnosis(t *testing.T) {
	_ = memberAPIConfig(t)
	original := perm.Matrix()
	t.Cleanup(func() { perm.Load(original) })
	changed := perm.Matrix()
	changed[perm.RoleAdmin][perm.MemberConfigure] = false
	changed[perm.RoleAdmin][perm.CensorManage] = false
	perm.Load(changed)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/membership/preview", store.DefaultMembershipConfig(), adminCSRF, adminCookie), 403)
	checkJSON(t, smokeGet(t, "/api/v1/admin/censor", adminCookie), 403)
	checkJSON(t, smokePost(t, "/api/v1/admin/censor/add", adminCSRF, "word=forbidden", adminCookie), 403)
	u, _, _ := memberTestUser(t)
	res := smokeGet(t, fmt.Sprintf("/api/v1/admin/membership/diagnose?userId=%d&forumId=1&action=thread.create", u.ID), adminCookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"allowed":true`) {
		t.Fatal("diagnosis did not evaluate target user")
	}
}
func TestMemberSSERevokesOnForumPolicyChange(t *testing.T) {
	c := memberAPIConfig(t)
	_, cookie, _ := memberTestUser(t)
	server := httptest.NewServer(smokeSrv.Handler())
	defer server.Close()
	request, _ := http.NewRequest("GET", server.URL+"/api/v1/events?forums=1", nil)
	request.AddCookie(cookie)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("SSE connection failed", resp.StatusCode)
	}
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: 1, MinimumLevel: 2, MembersOnly: true})
	setMemberAPIConfig(t, c)
	smokeSrv.publish("f:1", eventBody{Type: "thread.update", TID: 1, FID: 1})
	buf := make([]byte, 1024)
	body := ""
	for {
		n, err := resp.Body.Read(buf)
		body += string(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(body, "subscription.reset") || strings.Contains(body, "thread.update") {
		t.Fatalf("SSE leaked event after access revocation: %s", body)
	}
}

func TestMemberGuestAndAdjustmentPermissions(t *testing.T) {
	c := memberAPIConfig(t)
	u, cookie, _ := memberTestUser(t)
	c.GuestPermissions["forum.read"] = false
	setMemberAPIConfig(t, c)
	checkJSON(t, smokeGet(t, "/api/v1/threads/1", nil), 404)
	checkJSON(t, smokeGet(t, "/api/v1/threads/1", cookie), 200)
	original := perm.Matrix()
	t.Cleanup(func() { perm.Load(original) })
	changed := perm.Matrix()
	changed[perm.RoleAdmin][perm.MemberAdjust] = false
	changed[perm.RoleAdmin][perm.ExperienceAdjust] = false
	perm.Load(changed)
	m, err := smokeSrv.st.Membership(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "PATCH", fmt.Sprintf("/api/v1/admin/membership/users/%d", u.ID), store.MemberAdjustment{Version: m.Version, Delta: 5, Reason: "permission check", Key: "permission-check"}, adminCSRF, adminCookie), 403)
	checkJSON(t, memberJSON(t, "PATCH", fmt.Sprintf("/api/v1/admin/membership/users/%d", u.ID), store.MemberAdjustment{Version: m.Version, Reason: "empty operation", Key: "empty-operation"}, adminCSRF, adminCookie), 422)
}

func TestMemberForumModerationOverride(t *testing.T) {
	c := memberAPIConfig(t)
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	m, err := smokeSrv.st.Membership(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	lid := 2
	if err = smokeSrv.st.AdjustMember(ctx, u.ID, 1, store.MemberAdjustment{Version: m.Version, LevelID: &lid, Reason: "test senior member", Key: "moderation-test-level"}); err != nil {
		t.Fatal(err)
	}
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: 1, MinimumLevel: 0, Denied: []string{"post.link.direct"}})
	setMemberAPIConfig(t, c)
	res := memberJSON(t, "POST", "/api/v1/threads", map[string]any{"forumId": "1", "subject": "forum moderation policy", "content": "https://example.org/"}, csrf, cookie)
	checkJSON(t, res, 201)
	if !strings.Contains(res.Body.String(), `"pending":true`) {
		t.Fatal("forum failed to override senior link exemption")
	}
}

func TestMemberResponsesUseOnlyNewLevels(t *testing.T) {
	_ = memberAPIConfig(t)
	u, cookie, _ := memberTestUser(t)
	for _, path := range []string{"/api/v1/session", "/api/v1/me", fmt.Sprintf("/api/v1/users/%d", u.ID), "/api/v1/admin/users", "/api/v1/me/export"} {
		viewer := cookie
		if path == "/api/v1/admin/users" {
			viewer = adminCookie
		}
		response := smokeGet(t, path, viewer)
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if strings.Contains(body, "trustLevel") || strings.Contains(body, "trust_level") {
			t.Fatalf("%s returned legacy trust field", path)
		}
		if !strings.Contains(body, `"level"`) {
			t.Fatalf("%s is missing new level data", path)
		}
	}
	data := checkJSON(t, smokeGet(t, "/api/v1/membership/levels", nil), 200)
	var levels []store.MemberLevel
	if err := json.Unmarshal(data["data"], &levels); err != nil {
		t.Fatal(err)
	}
	if len(levels) != 5 {
		t.Fatal("expected five default member levels")
	}
	for i, xp := range []int64{0, 100, 500, 1500, 5000} {
		if levels[i].Experience != xp || levels[i].DaysVisited != 0 || levels[i].PostsRead != 0 || levels[i].PostCount != 0 {
			t.Fatalf("unexpected new level thresholds: %+v", levels[i])
		}
	}
}
