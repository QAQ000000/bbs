package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dzforum/internal/mailtest"
	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func emailAPIServer(t *testing.T) (*Server, *mailtest.SMTP) {
	t.Helper()
	requireDB(t)
	smtp := mailtest.New(t)
	cfg := smokeSrv.cfg
	cfg.SMTPHost = smtp.Host
	cfg.SMTPPort = smtp.Port
	cfg.SMTPFrom = "noreply@example.test"
	cfg.MailKey = strings.Repeat("ab", 32)
	srv, err := New(cfg, store.New(smokePool), smokeSrv.hub, smokeSrv.log)
	if err != nil {
		t.Fatal(err)
	}
	return srv, smtp
}

func emailAPIJob(t *testing.T, uid int64) *store.EmailJob {
	t.Helper()
	var j store.EmailJob
	err := smokePool.QueryRow(context.Background(), `SELECT id,uid,kind,status,attempts,version,recipient,sealed_token,token_hash,coalesce(post_id,0),expires_at FROM email_jobs WHERE uid=$1 ORDER BY id DESC LIMIT 1`, uid).Scan(&j.ID, &j.UID, &j.Kind, &j.Status, &j.Attempts, &j.Version, &j.Recipient, &j.SealedToken, &j.TokenHash, &j.PostID, &j.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return &j
}

func TestEmailAPIHTTPRestartSMTPAndAdminRetry(t *testing.T) {
	ctx := context.Background()
	u, _, _ := memberTestUser(t)
	srv, smtp := emailAPIServer(t)
	if _, err := smokePool.Exec(ctx, `DELETE FROM email_jobs`); err != nil {
		t.Fatal(err)
	}
	email := "restart@example.test"
	if _, err := srv.st.UpdateProfile(ctx, u.ID, "", email); err != nil {
		t.Fatal(err)
	}
	// A real loopback HTTP request enqueues the job without any running email worker.
	h := httptest.NewServer(srv.Handler())
	req, _ := http.NewRequest("POST", h.URL+"/api/v1/auth/password/forgot", strings.NewReader(`{"email":"restart@example.test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", adminCSRF)
	req.AddCookie(adminCookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	h.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, string(body))
	}
	j := emailAPIJob(t, u.ID)
	if j.Status != "pending" {
		t.Fatal(j)
	}
	// Recreate the server/store with the same key; the only surviving task state is PostgreSQL.
	restarted, err := New(srv.cfg, store.New(smokePool), smokeSrv.hub, smokeSrv.log)
	if err != nil {
		t.Fatal(err)
	}
	smtp.Reject.Store(451)
	if id, err := restarted.ProcessEmail(ctx); err != nil || id != j.ID {
		t.Fatal(id, err)
	}
	j = emailAPIJob(t, u.ID)
	if j.Status != "pending" || j.Attempts != 1 {
		t.Fatal(j)
	}
	if id, err := restarted.ProcessEmail(ctx); err != nil || id != 0 {
		t.Fatal("retry ignored backoff", id, err)
	}
	if _, err = smokePool.Exec(ctx, `UPDATE email_jobs SET next_attempt_at=now() WHERE id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	smtp.Reject.Store(550)
	if _, err = restarted.ProcessEmail(ctx); err != nil {
		t.Fatal(err)
	}
	j = emailAPIJob(t, u.ID)
	if j.Status != "dead" {
		t.Fatal(j)
	}
	original := smokeSrv
	smokeSrv = restarted
	t.Cleanup(func() { smokeSrv = original })
	path := fmt.Sprintf("/api/v1/admin/email-jobs/%d/retry", j.ID)
	checkJSON(t, smokeGet(t, "/api/v1/admin/email-jobs", userCookie), 403)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"version": j.Version}, "", adminCookie), 403)
	response := smokeGet(t, "/api/v1/admin/email-jobs?status=dead", adminCookie)
	checkJSON(t, response, 200)
	if strings.Contains(response.Body.String(), email) || strings.Contains(response.Body.String(), j.SealedToken) || strings.Contains(response.Body.String(), "private SMTP") {
		t.Fatal("admin disclosed private payload")
	}
	originalPerm := perm.Matrix()
	t.Cleanup(func() { perm.Load(originalPerm) })
	changed := perm.Matrix()
	changed[perm.RoleAdmin][perm.EmailManage] = false
	perm.Load(changed)
	checkJSON(t, smokeGet(t, "/api/v1/admin/email-jobs", adminCookie), 403)
	perm.Load(originalPerm)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"version": j.Version}, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"version": j.Version}, adminCSRF, adminCookie), 409)
	smtp.Reject.Store(0)
	if _, err = restarted.ProcessEmail(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-smtp.Messages:
		if !strings.Contains(body, "/reset?token=") {
			t.Fatal("missing reset link")
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP received no message")
	}
	j = emailAPIJob(t, u.ID)
	if j.Status != "sent" || j.SealedToken != "" {
		t.Fatal("sent token retained", j.Status)
	}
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"version": j.Version}, adminCSRF, adminCookie), 409)
}

func TestEmailQueueRevalidatesTokenAndRecipient(t *testing.T) {
	ctx := context.Background()
	srv, _ := emailAPIServer(t)
	u, _, _ := memberTestUser(t)
	email := "invalidate@example.test"
	if _, err := srv.st.UpdateProfile(ctx, u.ID, "", email); err != nil {
		t.Fatal(err)
	}
	queue := func() {
		t.Helper()
		if err := srv.st.QueueAuthEmail(ctx, u.ID, email, "email_verify", srv.mailTokens.Seal); err != nil {
			t.Fatal(err)
		}
	}
	queue()
	first := emailAPIJob(t, u.ID)
	queue()
	if _, err := srv.prepareEmail(ctx, first); !errors.Is(err, errEmailCancelled) {
		t.Fatal("superseded token remained deliverable", err)
	}
	current := emailAPIJob(t, u.ID)
	msg, err := srv.prepareEmail(ctx, current)
	if err != nil || !strings.Contains(msg.Link, "/verify?token=") {
		t.Fatal(msg, err)
	}
	if _, err = srv.st.UpdateProfile(ctx, u.ID, "", "changed@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.prepareEmail(ctx, current); !errors.Is(err, errEmailCancelled) {
		t.Fatal("old address remained deliverable", err)
	}
	email = "changed@example.test"
	if err = srv.st.QueueAuthEmail(ctx, u.ID, email, "password_reset", srv.mailTokens.Seal); err != nil {
		t.Fatal(err)
	}
	reset := emailAPIJob(t, u.ID)
	raw, err := srv.mailTokens.Open(reset.SealedToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.st.ResetPasswordByToken(ctx, raw, "new-password123"); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.prepareEmail(ctx, reset); !errors.Is(err, errEmailCancelled) {
		t.Fatal("used reset remained deliverable", err)
	}
}

func TestEmailQueueRevalidatesContentAndPreferences(t *testing.T) {
	ctx := context.Background()
	srv, _ := emailAPIServer(t)
	u, _, _ := memberTestUser(t)
	c := memberAPIConfig(t)
	email := "content@example.test"
	if _, err := srv.st.UpdateProfile(ctx, u.ID, "", email); err != nil {
		t.Fatal(err)
	}
	th, p, err := srv.st.CreateThread(ctx, 1, 1, "admin", "notification topic", "notification body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	n := &store.Notification{UID: u.ID, FromUID: 1, FromName: "admin", Type: "mention", ThreadID: th.ID, PostID: p.ID, EventKey: fmt.Sprint("post:", p.ID), Email: true}
	if err = srv.st.AddNotifications(ctx, []*store.Notification{n}); err != nil {
		t.Fatal(err)
	}
	j := emailAPIJob(t, u.ID)
	if msg, err := srv.prepareEmail(ctx, j); err != nil || msg.Excerpt != "notification body" {
		t.Fatal(msg, err)
	}
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: 1, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, c)
	if _, err = srv.prepareEmail(ctx, j); !errors.Is(err, errEmailCancelled) {
		t.Fatal("restricted content leak", err)
	}
	c.Forums = c.Forums[:len(c.Forums)-1]
	setMemberAPIConfig(t, c)
	if err = srv.st.SaveNotificationPreferences(ctx, u.ID, map[string]bool{"email": false}); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.prepareEmail(ctx, j); !errors.Is(err, errEmailCancelled) {
		t.Fatal("global opt-out ignored", err)
	}
	if err = srv.st.SaveNotificationPreferences(ctx, u.ID, map[string]bool{"email": true}); err != nil {
		t.Fatal(err)
	}
	if _, err = smokePool.Exec(ctx, `UPDATE posts SET pending=true WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.prepareEmail(ctx, j); !errors.Is(err, errEmailCancelled) {
		t.Fatal("pending content leak", err)
	}
	if _, err = smokePool.Exec(ctx, `UPDATE posts SET pending=false,deleted=true WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.prepareEmail(ctx, j); !errors.Is(err, errEmailCancelled) {
		t.Fatal("deleted content leak", err)
	}
}

func TestEmailOnlySubscriptionDurabilityAndUnsubscribe(t *testing.T) {
	ctx := context.Background()
	srv, _ := emailAPIServer(t)
	u, _, _ := memberTestUser(t)
	if _, err := srv.st.UpdateProfile(ctx, u.ID, "", "subscription@example.test"); err != nil {
		t.Fatal(err)
	}
	th, _, err := srv.st.CreateThread(ctx, 1, 1, "admin", "subscription mail", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	v := store.Subscription{Kind: "thread", TargetID: th.ID, Enabled: true, NotifyInApp: false, NotifyEmail: true}
	if _, err = srv.st.SaveSubscription(ctx, u.ID, v, false); err != nil {
		t.Fatal(err)
	}
	_, p, err := srv.st.CreateReply(ctx, th.ID, 1, "admin", "mail-only reply", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		id, err := srv.ProcessSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if id == 0 {
			break
		}
	}
	j := emailAPIJob(t, u.ID)
	if j.PostID != p.ID || j.Kind != "subscription" {
		t.Fatal(j)
	}
	var n int
	if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2`, u.ID, p.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("email-only subscription created in-app notification", n, err)
	}
	if _, err = srv.prepareEmail(ctx, j); err != nil {
		t.Fatal(err)
	}
	v.Enabled = false
	if _, err = srv.st.SaveSubscription(ctx, u.ID, v, false); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.prepareEmail(ctx, j); !errors.Is(err, errEmailCancelled) {
		t.Fatal("unsubscribe ignored", err)
	}
	b, _ := json.Marshal(j)
	if strings.Contains(string(b), "subscription@example.test") {
		t.Fatal("address exposed")
	}
}
