package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDeviceSessionAPI(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	second, csrf2, err := smokeSrv.st.CreateDeviceSession(ctx, u.ID, "Firefox test", "192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	otherCookie := &http.Cookie{Name: cookieSession, Value: second}
	sess, err := smokeSrv.st.Session(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/me/sessions/%d", sess.ID)
	checkJSON(t, smokeGet(t, "/api/v1/me/sessions", nil), 401)
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"name": "Laptop"}, "", cookie), 403)
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"name": "Laptop"}, adminCSRF, adminCookie), 404)
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"name": "Laptop"}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"name": strings.Repeat("x", 81)}, csrf, cookie), 422)
	response := smokeGet(t, "/api/v1/me/sessions", cookie)
	checkJSON(t, response, 200)
	for _, secret := range []string{cookie.Value, second, sess.Token, sess.CSRF} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("session secret exposed")
		}
	}
	if !strings.Contains(response.Body.String(), `"name":"Laptop"`) || !strings.Contains(response.Body.String(), `"current":true`) {
		t.Fatal(response.Body.String())
	}
	// Revoke others and immediately use the old cookie through full middleware.
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/sessions/revoke-others", map[string]any{}, csrf, cookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/me", otherCookie), 401)
	checkJSON(t, memberJSON(t, "DELETE", "/api/v1/me/sessions", map[string]any{}, csrf2, otherCookie), 401)
	checkJSON(t, smokeGet(t, "/api/v1/me", cookie), 200)
	data := checkJSON(t, memberJSON(t, "DELETE", "/api/v1/me/sessions", map[string]any{}, csrf, cookie), 200)
	var result struct {
		SignedOut bool `json:"signedOut"`
	}
	_ = json.Unmarshal(data["data"], &result)
	if !result.SignedOut {
		t.Fatal("all-device logout did not clear current session")
	}
	checkJSON(t, smokeGet(t, "/api/v1/me", cookie), 401)
}

func TestDeviceSessionActivityAndLogoutCSRF(t *testing.T) {
	ctx := context.Background()
	_, cookie, csrf := memberTestUser(t)
	sess, err := smokeSrv.st.Session(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = smokePool.Exec(ctx, `UPDATE sessions SET last_seen_at=now()-interval '5 minutes' WHERE id=$1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/me/sessions", cookie), 200)
	var recent bool
	if err = smokePool.QueryRow(ctx, `SELECT last_seen_at>now()-interval '5 seconds' FROM sessions WHERE id=$1`, sess.ID).Scan(&recent); err != nil || !recent {
		t.Fatal(recent, err)
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/auth/logout", map[string]any{}, "", cookie), 403)
	checkJSON(t, smokeGet(t, "/api/v1/me", cookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/auth/logout", map[string]any{}, csrf, cookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/me", cookie), 401)
}
