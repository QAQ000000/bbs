package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDeviceSessionsOwnershipRevocationAndRestart(t *testing.T) {
	ctx := context.Background()
	uid, other := setupUsers(t)
	raw, _, err := testStore.CreateDeviceSession(ctx, uid, "browser\r\nagent", "192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := testStore.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	stranger, _, err := testStore.CreateSession(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := testStore.Session(ctx, raw)
	b, _ := testStore.Session(ctx, second)
	o, _ := testStore.Session(ctx, stranger)
	restarted := New(testPool)
	if _, err = restarted.SessionCached(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.ManageDevice(ctx, uid, a.ID, o.ID, "revoke", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user revoke", err)
	}
	if _, err = testStore.ManageDevice(ctx, uid, a.ID, b.ID, "rename", "Office laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.ManageDevice(ctx, uid, a.ID, b.ID, "rename", "bad\nname"); !errors.Is(err, ErrDeviceName) {
		t.Fatal(err)
	}
	if _, err = testStore.ManageDevice(ctx, uid, a.ID, b.ID, "revoke", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.SessionCached(ctx, second); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked session remained cached", err)
	}
	if _, err = testStore.ManageDevice(ctx, uid, b.ID, a.ID, "revoke", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked actor remained authorized", err)
	}
	rows, err := restarted.DeviceSessions(ctx, uid, a.ID, 0)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	data, _ := json.Marshal(rows)
	for _, secret := range []string{raw, second, a.Token, a.CSRF} {
		if strings.Contains(string(data), secret) {
			t.Fatal("session secret leaked")
		}
	}
	if rows[0].Status != "revoked" || rows[0].Name != "Office laptop" || !rows[1].Current || rows[1].UserAgent != "browseragent" {
		t.Fatal(rows)
	}
	if n, err := testStore.ManageDevice(ctx, uid, a.ID, 0, "all", ""); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = restarted.Session(ctx, raw); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = restarted.Session(ctx, stranger); err != nil {
		t.Fatal("other account affected", err)
	}
}

func TestDeviceSessionPasswordBlockAndExpiry(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	u, _ := testStore.UserByID(ctx, uid)
	a, _, _ := testStore.CreateSession(ctx, uid)
	b, _, _ := testStore.CreateSession(ctx, uid)
	if err := testStore.ChangePassword(ctx, uid, "pass123456", "new-password123", a); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.Session(ctx, b); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := testStore.Session(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStore.CreateDeviceSession(ctx, uid, "", "", u.PasswordHash); !errors.Is(err, ErrWrongPassword) {
		t.Fatal("old password raced reset", err)
	}
	if err := testStore.BlockUser(ctx, uid, 0); err != nil {
		t.Fatal(err)
	}
	if err := testStore.UnblockUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.Session(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatal("unblock resurrected device", err)
	}
	raw, _, _ := testStore.CreateSession(ctx, uid)
	if _, err := testPool.Exec(ctx, `UPDATE sessions SET expires_at=now()-interval '1 second' WHERE token=$1`, hashToken(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.SessionCached(ctx, raw); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session accepted", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE sessions SET revoked_at=now()-interval '31 days' WHERE user_id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	testStore.PurgeSessions(ctx)
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, uid).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
