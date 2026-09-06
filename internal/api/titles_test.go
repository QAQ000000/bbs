// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func apiTitleDefinition() store.TitleDefinition {
	return store.TitleDefinition{Name: "Helpful member", Description: "One accepted reply", Badge: store.LevelBadge{Label: "Helpful", Icon: "star", Color: "#112233", Background: "#ddeeff"}, Status: "draft", Mode: "automatic", Match: "all", Conditions: []store.TitleCondition{{Metric: "accepted_replies", Target: 1}}}
}
func decodeTitle(t *testing.T, data map[string]json.RawMessage) store.TitleDefinition {
	t.Helper()
	var c store.TitleDefinition
	if err := json.Unmarshal(data["data"], &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTitlesAPIConfigurationAcceptanceAndDisplay(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	owner, ownerCookie, ownerCSRF := memberTestUser(t)
	answerer, err := smokeSrv.st.CreateUser(ctx, "title_answerer", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, owner.ID, owner.Username, "title question", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := smokeSrv.st.CreateReply(ctx, th.ID, answerer.ID, answerer.Username, "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	c := apiTitleDefinition()
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/titles", c, "", adminCookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/titles", c, ownerCSRF, ownerCookie), 403)
	c = decodeTitle(t, checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/titles", c, adminCSRF, adminCookie), 201))
	if strings.Contains(smokeGet(t, "/api/v1/titles", nil).Body.String(), c.Name) {
		t.Fatal("draft publicly visible")
	}
	c.Status = "active"
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/titles/preview", c, adminCSRF, adminCookie), 200)
	path := fmt.Sprintf("/api/v1/admin/titles/%d", c.ID)
	old := c
	c = decodeTitle(t, checkJSON(t, memberJSON(t, "PUT", path, c, adminCSRF, adminCookie), 200))
	checkJSON(t, memberJSON(t, "PUT", path, old, adminCSRF, adminCookie), 409)
	accept := fmt.Sprintf("/api/v1/posts/%d/acceptance", p.ID)
	checkJSON(t, memberJSON(t, "PUT", accept, map[string]any{}, "", ownerCookie), 403)
	checkJSON(t, memberJSON(t, "PUT", accept, map[string]any{}, adminCSRF, adminCookie), 403)
	res := smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", p.ID), ownerCookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), `"canAccept":true`) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, memberJSON(t, "PUT", accept, map[string]any{}, ownerCSRF, ownerCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", accept, map[string]any{}, ownerCSRF, ownerCookie), 200)
	for i := 0; i < 100; i++ {
		n, e := smokeSrv.st.ProcessTitleWork(ctx, 100)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
	}
	if err = smokeSrv.st.EquipTitle(ctx, answerer.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{fmt.Sprintf("/api/v1/posts/%d", p.ID), fmt.Sprintf("/api/v1/threads/%d/posts", th.ID)} {
		res = smokeGet(t, url, ownerCookie)
		checkJSON(t, res, 200)
		if !strings.Contains(res.Body.String(), `"accepted":true`) || !strings.Contains(res.Body.String(), `"equippedTitle":{`) || !strings.Contains(res.Body.String(), c.Name) {
			t.Fatal(res.Body.String())
		}
	}
	checkJSON(t, smokeGet(t, path+"/jobs", adminCookie), 200)
	checkJSON(t, smokeGet(t, path+"/logs", adminCookie), 200)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/admin/users/%d/titles", answerer.ID), adminCookie), 200)
	checkJSON(t, memberJSON(t, "DELETE", accept, map[string]any{}, ownerCSRF, ownerCookie), 200)
	c.Status = "disabled"
	checkJSON(t, memberJSON(t, "PUT", path, c, adminCSRF, adminCookie), 200)
	res = smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", p.ID), ownerCookie)
	checkJSON(t, res, 200)
	if strings.Contains(res.Body.String(), `"equippedTitle":{`) {
		t.Fatal("disabled title still displayed")
	}
}

func TestTitlesAPIManualPermissionsAndEquipment(t *testing.T) {
	requireDB(t)
	u, cookie, csrf := memberTestUser(t)
	c := apiTitleDefinition()
	c.Mode = "manual"
	c.Conditions = []store.TitleCondition{}
	c = decodeTitle(t, checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/titles", c, adminCSRF, adminCookie), 201))
	c.Status = "active"
	c = decodeTitle(t, checkJSON(t, memberJSON(t, "PUT", fmt.Sprintf("/api/v1/admin/titles/%d", c.ID), c, adminCSRF, adminCookie), 200))
	unearned := smokeGet(t, "/api/v1/me/titles", cookie)
	checkJSON(t, unearned, 200)
	if !strings.Contains(unearned.Body.String(), `"status":"not_earned"`) {
		t.Fatal("manual title incorrectly has task progress", unearned.Body.String())
	}
	path := fmt.Sprintf("/api/v1/admin/users/%d/titles/%d", u.ID, c.ID)
	a := store.TitleAdjustment{Action: "grant", Reason: "contribution", Key: "api-title-grant-once", Version: c.Version}
	checkJSON(t, memberJSON(t, "PATCH", path, a, csrf, cookie), 403)
	before := perm.Matrix()
	t.Cleanup(func() { perm.Load(before) })
	changed := perm.Matrix()
	changed[perm.RoleAdmin][perm.TitleGrant] = false
	perm.Load(changed)
	checkJSON(t, memberJSON(t, "PATCH", path, a, adminCSRF, adminCookie), 403)
	perm.Load(before)
	checkJSON(t, memberJSON(t, "PATCH", path, a, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PATCH", path, a, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/title", map[string]any{}, csrf, cookie), 422)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/title", map[string]string{"titleId": fmt.Sprint(c.ID)}, "", cookie), 403)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/title", map[string]string{"titleId": fmt.Sprint(c.ID)}, csrf, cookie), 200)
	res := smokeGet(t, "/api/v1/me", cookie)
	checkJSON(t, res, 200)
	if !strings.Contains(res.Body.String(), c.Name) {
		t.Fatal(res.Body.String())
	}
	checkJSON(t, smokeGet(t, "/api/v1/me/titles", cookie), 200)
	a.Action = "revoke"
	a.Key = "api-title-revoke-once"
	changed = perm.Matrix()
	changed[perm.RoleAdmin][perm.TitleRevoke] = false
	perm.Load(changed)
	checkJSON(t, memberJSON(t, "PATCH", path, a, adminCSRF, adminCookie), 403)
	perm.Load(before)
	checkJSON(t, memberJSON(t, "PATCH", path, a, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/title", map[string]string{"titleId": fmt.Sprint(c.ID)}, csrf, cookie), 403)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/me/title", map[string]string{"titleId": "0"}, csrf, cookie), 200)
}

func TestTitlesAcceptanceVisibilityAndRolePermission(t *testing.T) {
	c := memberAPIConfig(t)
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	th, _, err := smokeSrv.st.CreateThread(ctx, smokeFid2, u.ID, u.Username, "restricted question", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := smokeSrv.st.CreateReply(ctx, th.ID, 1, "admin", "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/posts/%d/acceptance", p.ID)
	before := perm.Matrix()
	t.Cleanup(func() { perm.Load(before) })
	changed := perm.Matrix()
	changed[perm.RoleMember][perm.ReplyAccept] = false
	perm.Load(changed)
	checkJSON(t, memberJSON(t, "PUT", path, map[string]any{}, csrf, cookie), 403)
	perm.Load(before)
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: smokeFid2, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, c)
	checkJSON(t, memberJSON(t, "PUT", path, map[string]any{}, csrf, cookie), 404)
	checkJSON(t, memberJSON(t, "PUT", path, map[string]any{}, "", nil), 401)
}
