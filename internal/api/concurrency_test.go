package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"dzforum/internal/store"
)

func TestConcurrencyLiveAuthorizationSingleQuery(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	u, cookie, _ := memberTestUser(t)
	th, p, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "live auth", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/events?thread=%d&forums=1&user=%d", th.ID, u.ID), nil)
	r.AddCookie(cookie)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
	before := smokePool.Stat().AcquireCount()
	if !smokeSrv.liveAuthorized(r) {
		t.Fatal("valid session denied")
	}
	if queries := smokePool.Stat().AcquireCount() - before; queries != 1 {
		t.Fatal("live authorization acquired more than one connection", queries)
	}
	if _, err = smokePool.Exec(ctx, `UPDATE users SET blocked_until=now()+interval '1 hour' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if smokeSrv.liveAuthorized(r) {
		t.Fatal("blocked subscriber allowed")
	}
	if _, err = smokePool.Exec(ctx, `UPDATE users SET blocked_until=NULL WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.SetPostPendingModeration(ctx, p.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if !smokeSrv.liveAuthorized(r) {
		t.Fatal("author cannot view own pending thread")
	}
	other := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/events?thread=%d", th.ID), nil)
	if smokeSrv.liveAuthorized(other) {
		t.Fatal("guest saw pending thread")
	}
	if err = smokeSrv.st.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if !smokeSrv.liveAuthorized(other) {
		t.Fatal("public thread denied")
	}
	c := memberAPIConfig(t)
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: 1, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, c)
	if smokeSrv.liveAuthorized(r) || smokeSrv.liveAuthorized(other) {
		t.Fatal("new forum restriction ignored")
	}
	admin := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/events?thread=%d", th.ID), nil)
	admin.AddCookie(adminCookie)
	admin = admin.WithContext(context.WithValue(ctx, ctxUser, &store.User{ID: 1}))
	if !smokeSrv.liveAuthorized(admin) {
		t.Fatal("admin override missing")
	}
	// A valid cookie for another user cannot inherit the original user's private channel.
	wrong := httptest.NewRequest("GET", "/api/v1/events?user=1", nil)
	wrong.AddCookie(&http.Cookie{Name: cookieSession, Value: cookie.Value})
	wrong = wrong.WithContext(context.WithValue(ctx, ctxUser, &store.User{ID: 1}))
	if smokeSrv.liveAuthorized(wrong) {
		t.Fatal("session/user mismatch accepted")
	}
}
