package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAPIContentInteractions(t *testing.T) {
	requireDB(t)
	doc := loadAPIContract(t)
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	th, post, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "content contract", "original body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	tid, pid := idString(th.ID), idString(post.ID)
	for _, path := range []string{"/api/v1/site", "/api/v1/home", "/api/v1/forums"} {
		assertContractResponse(t, doc, "GET", path, smokeGet(t, path, nil), 200)
	}
	assertContractResponse(t, doc, "GET", "/api/v1/forums/{fid}", smokeGet(t, "/api/v1/forums/1", cookie), 200)
	assertContractResponse(t, doc, "GET", "/api/v1/forums/{fid}", smokeGet(t, "/api/v1/forums/999999999", nil), 404)
	likePath := "/api/v1/posts/" + pid + "/like"
	assertContractResponse(t, doc, "POST", "/api/v1/posts/{pid}/like", memberJSON(t, "POST", likePath, map[string]any{}, "", userCookie), 403)
	assertContractResponse(t, doc, "POST", "/api/v1/posts/{pid}/like", memberJSON(t, "POST", likePath, map[string]any{}, csrf, cookie), 403)
	for _, want := range []bool{true, false, true} {
		body := assertContractResponse(t, doc, "POST", "/api/v1/posts/{pid}/like", memberJSON(t, "POST", likePath, map[string]any{}, userCSRF, userCookie), 200)
		if body["data"].(contractObject)["liked"] != want {
			t.Fatal("toggle semantics changed", body)
		}
	}
	likers := assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}/likes", smokeGet(t, "/api/v1/posts/"+pid+"/likes", nil), 200)["data"].([]any)
	if len(likers) != 1 || likers[0].(contractObject)["name"] != "user01" {
		t.Fatal("missing liker", likers)
	}
	assertContractResponse(t, doc, "POST", "/api/v1/threads/{tid}/favorite", memberJSON(t, "POST", "/api/v1/threads/"+tid+"/favorite", map[string]any{}, csrf, cookie), 200)
	favorites := assertContractResponse(t, doc, "GET", "/api/v1/me/favorites", smokeGet(t, "/api/v1/me/favorites", cookie), 200)["data"].([]any)
	if len(favorites) != 1 || favorites[0].(contractObject)["id"] != tid {
		t.Fatal("favorite missing from list", favorites)
	}
	assertContractResponse(t, doc, "GET", "/api/v1/me/favorites", smokeGet(t, "/api/v1/me/favorites", nil), 401)
	assertContractResponse(t, doc, "PATCH", "/api/v1/posts/{pid}", memberJSON(t, "PATCH", "/api/v1/posts/"+pid, map[string]any{"subject": th.Title, "content": "edited body", "version": post.Version}, csrf, cookie), 200)
	historyPath := "/api/v1/posts/" + pid + "/history"
	history := assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}/history", smokeGet(t, historyPath, cookie), 200)["data"].([]any)
	if len(history) != 1 || history[0].(contractObject)["content"] != "original body" {
		t.Fatal("history must contain pre-edit body", history)
	}
	assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}/history", smokeGet(t, historyPath, nil), 401)
	assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}/history", smokeGet(t, historyPath, userCookie), 403)
	_, reply, err := smokeSrv.st.CreateReply(ctx, th.ID, u.ID, u.Username, "reply to remove", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	position := assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}/position", smokeGet(t, fmt.Sprintf("/api/v1/posts/%d/position", reply.ID), nil), 200)["data"].(contractObject)
	if position["threadId"] != tid || position["page"] != float64(1) {
		t.Fatal(position)
	}
	deleted := assertContractResponse(t, doc, "DELETE", "/api/v1/posts/{pid}", memberJSON(t, "DELETE", "/api/v1/posts/"+idString(reply.ID), map[string]any{}, csrf, cookie), 200)
	if len(deleted["data"].(contractObject)) != 0 {
		t.Fatal("reply deletion returns an empty action result", deleted)
	}
	assertContractResponse(t, doc, "DELETE", "/api/v1/posts/{pid}", memberJSON(t, "DELETE", "/api/v1/posts/"+pid, map[string]any{}, csrf, cookie), 200)
	assertContractResponse(t, doc, "GET", "/api/v1/threads/{tid}", smokeGet(t, "/api/v1/threads/"+tid, nil), 404)
}

func TestOpenAPIModerationWorkflow(t *testing.T) {
	requireDB(t)
	doc := loadAPIContract(t)
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	th, post, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "pending contract", "pending body", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	_, pendingReply, err := smokeSrv.st.CreateReply(ctx, 1, u.ID, u.Username, "pending reply", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	reportPath := fmt.Sprintf("/api/v1/posts/%d/reports", smokePid2)
	assertContractResponse(t, doc, "POST", "/api/v1/posts/{pid}/reports", memberJSON(t, "POST", reportPath, map[string]any{"reason": "outside moderator scope"}, csrf, cookie), 200)
	assertContractResponse(t, doc, "POST", "/api/v1/posts/{pid}/reports", memberJSON(t, "POST", "/api/v1/posts/1/reports", map[string]any{"reason": strings.Repeat("理", 201)}, csrf, cookie), 200)
	queuePath := "/api/v1/admin/moderate"
	assertContractResponse(t, doc, "GET", queuePath, smokeGet(t, queuePath, cookie), 403)
	queue := assertContractResponse(t, doc, "GET", queuePath, smokeGet(t, queuePath, modCookie), 200)["data"].(contractObject)
	for _, name := range []string{"threads", "posts", "reports"} {
		if len(queue[name].([]any)) == 0 {
			t.Fatal("empty fixture cannot verify queue contract", name)
		}
	}
	var reportID string
	for _, row := range queue["reports"].([]any) {
		r := row.(contractObject)
		if r["tId"] == idString(smokeTid2) {
			t.Fatal("out-of-scope report visible")
		}
		if r["reporterId"] == idString(u.ID) && r["postId"] == "1" {
			reportID = r["id"].(string)
			if r["reason"] != strings.Repeat("理", 200)+"…" {
				t.Fatal("report reason must preserve the first 200 characters plus ellipsis")
			}
		}
	}
	if reportID == "" {
		t.Fatal("report missing")
	}
	adminQueue := assertContractResponse(t, doc, "GET", queuePath, smokeGet(t, queuePath, adminCookie), 200)["data"].(contractObject)
	for _, row := range adminQueue["reports"].([]any) {
		r := row.(contractObject)
		if r["tId"] == idString(smokeTid2) {
			assertContractResponse(t, doc, "POST", "/api/v1/admin/report/handle", memberJSON(t, "POST", "/api/v1/admin/report/handle", map[string]any{"id": r["id"], "op": "dismiss"}, modCSRF, modCookie), 403)
		}
	}
	moderate := "/api/v1/admin/moderate/thread"
	body := map[string]any{"tid": idString(th.ID), "op": "approve"}
	assertContractResponse(t, doc, "POST", moderate, memberJSON(t, "POST", moderate, body, "", modCookie), 403)
	body["note"] = strings.Repeat("字", 501)
	assertContractResponse(t, doc, "POST", moderate, memberJSON(t, "POST", moderate, body, modCSRF, modCookie), 422)
	delete(body, "note")
	assertContractResponse(t, doc, "POST", moderate, memberJSON(t, "POST", moderate, body, modCSRF, modCookie), 200)
	assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}", smokeGet(t, "/api/v1/posts/"+idString(post.ID), nil), 200)
	moderate = "/api/v1/admin/moderate/post"
	assertContractResponse(t, doc, "POST", moderate, memberJSON(t, "POST", moderate, map[string]any{"pid": idString(post.ID), "op": "approve"}, modCSRF, modCookie), 422)
	assertContractResponse(t, doc, "POST", moderate, memberJSON(t, "POST", moderate, map[string]any{"pid": idString(pendingReply.ID), "op": "delete", "note": "please revise"}, modCSRF, modCookie), 200)
	assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}", smokeGet(t, "/api/v1/posts/"+idString(pendingReply.ID), cookie), 404)
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		assertContractResponse(t, doc, "POST", "/api/v1/admin/report/handle", memberJSON(t, "POST", "/api/v1/admin/report/handle", map[string]any{"id": reportID, "op": "dismiss"}, modCSRF, modCookie), status)
	}
}

func TestContentStaffRequiresInitialPasswordChange(t *testing.T) {
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	if _, err := smokePool.Exec(ctx, `UPDATE users SET group_id=2,must_change_password=true WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	doc := loadAPIContract(t)
	assertContractResponse(t, doc, "GET", "/api/v1/admin/moderate", smokeGet(t, "/api/v1/admin/moderate", cookie), 403)
	for _, tc := range []struct {
		path string
		body map[string]any
	}{
		{"/api/v1/admin/moderate/thread", map[string]any{"tid": "1", "op": "approve"}},
		{"/api/v1/admin/moderate/post", map[string]any{"pid": "2", "op": "approve"}},
		{"/api/v1/admin/report/handle", map[string]any{"id": "1", "op": "dismiss"}},
	} {
		response := assertContractResponse(t, doc, "POST", tc.path, memberJSON(t, "POST", tc.path, tc.body, csrf, cookie), 403)
		if !strings.Contains(response["error"].(contractObject)["message"].(string), "密码") {
			t.Fatal("expected password guard, not a later scope denial", response)
		}
	}
	if _, err := smokePool.Exec(ctx, `UPDATE users SET must_change_password=false WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	assertContractResponse(t, doc, "GET", "/api/v1/admin/moderate", smokeGet(t, "/api/v1/admin/moderate", cookie), 200)
}
