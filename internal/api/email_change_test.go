package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"

	"dzforum/internal/mfa"
	"dzforum/internal/store"
)

func emailChangeReceived(t *testing.T, raw, recipient string) string {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	to, err := msg.Header.AddressList("To")
	if err != nil || len(to) != 1 || to[0].Address != recipient {
		t.Fatal("incorrect email recipient", to, err)
	}
	body, err := io.ReadAll(msg.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestEmailChangeProofConfirmationAndSMTP(t *testing.T) {
	u, cookie, csrf := memberTestUser(t)
	ctx := context.Background()
	old := fmt.Sprintf("old-%d@example.test", u.ID)
	newEmail := fmt.Sprintf("new-%d@example.test", u.ID)
	flowSQL(t, `UPDATE users SET email=$2,signature='keep signature',email_verified=true WHERE id=$1`, u.ID, old)
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/me", map[string]any{"email": newEmail}, csrf, cookie), 422)
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/me", map[string]any{}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", map[string]any{"email": newEmail, "password": "password123"}, csrf, cookie), 503)
	srv, smtp := emailAPIServer(t)
	original := smokeSrv
	smokeSrv = srv
	t.Cleanup(func() { smokeSrv = original })
	flowSQL(t, `DELETE FROM email_jobs`)
	body := map[string]any{"email": newEmail, "password": "password123"}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", body, "", cookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", map[string]any{"email": newEmail}, csrf, cookie), 401)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", body, csrf, cookie), 202)
	changed, err := srv.st.UserByID(ctx, u.ID)
	if err != nil || changed.Email != old || !changed.EmailVerified || changed.Signature != "keep signature" {
		t.Fatal(changed, err)
	}
	j := emailAPIJob(t, u.ID)
	raw, err := srv.mailTokens.Open(j.SealedToken)
	if err != nil {
		t.Fatal(err)
	}
	// Restart construction must preserve the durable confirmation task.
	restarted, err := New(srv.cfg, store.New(smokePool), srv.hub, srv.log)
	if err != nil {
		t.Fatal(err)
	}
	smokeSrv = restarted
	if _, err = restarted.ProcessEmail(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-smtp.Messages:
		if !strings.Contains(emailChangeReceived(t, msg, newEmail), "/settings/email/confirm?token="+url.QueryEscape(raw)) {
			t.Fatal("invalid confirmation email")
		}
	case <-time.After(time.Second):
		t.Fatal("missing confirmation email")
	}
	oldReset, err := srv.st.CreatePasswordReset(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldVerify, err := srv.st.CreateEmailVerify(ctx, u.ID, old)
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, otherCSRF, err := srv.st.CreateDeviceSession(ctx, u.ID, "other", "")
	if err != nil {
		t.Fatal(err)
	}
	other := &http.Cookie{Name: cookieSession, Value: otherRaw}
	confirm := map[string]any{"token": raw}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/confirm", confirm, otherCSRF, other), 422)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/confirm", confirm, "", cookie), 403)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/confirm", confirm, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/confirm", confirm, csrf, cookie), 422)
	changed, err = srv.st.UserByID(ctx, u.ID)
	if err != nil || changed.Email != newEmail || !changed.EmailVerified || changed.Signature != "keep signature" {
		t.Fatal(changed, err)
	}
	if _, err = srv.st.ResetPasswordByToken(ctx, oldReset, "attacker-password"); !errors.Is(err, store.ErrTokenInvalid) {
		t.Fatal("old reset survived", err)
	}
	if _, _, err = srv.st.ConsumeEmailVerify(ctx, oldVerify); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("old verification survived", err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/me", other), 401)
	checkJSON(t, smokeGet(t, "/api/v1/me", cookie), 200)
	if _, err = restarted.ProcessEmail(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-smtp.Messages:
		if strings.Contains(emailChangeReceived(t, msg, old), raw) {
			t.Fatal("invalid security notice")
		}
	case <-time.After(time.Second):
		t.Fatal("missing old-address security notice")
	}
}

func TestEmailChangeRequiresMFAAndRejectsReplay(t *testing.T) {
	mfaTestServer(t)
	srv, _ := emailAPIServer(t)
	original := smokeSrv
	smokeSrv = srv
	t.Cleanup(func() { smokeSrv = original })
	u, cookie, csrf := memberTestUser(t)
	setup := mfaAPIData(t, memberJSON(t, "POST", "/api/v1/me/2fa/setup", map[string]any{"password": "password123"}, csrf, cookie), 200)
	enabled := mfaAPIData(t, memberJSON(t, "POST", "/api/v1/me/2fa/enable", map[string]any{"password": "password123", "setupId": setup["setupId"], "code": mfa.Code(setup["secret"].(string), time.Now().Unix()/30)}, csrf, cookie), 200)
	body := map[string]any{"email": fmt.Sprintf("mfa-%d@example.test", u.ID), "password": "password123"}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", body, csrf, cookie), 401)
	body["recovery"] = enabled["recoveryCodes"].([]any)[0]
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", body, csrf, cookie), 202)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/change", body, csrf, cookie), 401)
	j := emailAPIJob(t, u.ID)
	raw, err := srv.mailTokens.Open(j.SealedToken)
	if err != nil {
		t.Fatal(err)
	}
	// A later MFA configuration change invalidates previously issued email proofs.
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/2fa/recovery-codes", map[string]any{"password": "password123", "recovery": enabled["recoveryCodes"].([]any)[1]}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/email/confirm", map[string]any{"token": raw}, csrf, cookie), 422)
}
