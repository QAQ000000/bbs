package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dzforum/internal/mfa"
)

type mfaFixture struct {
	uid, sid            int64
	token, hash, secret string
	codes               []string
	cipher              *mfa.Cipher
}

func setupMFA(t *testing.T) mfaFixture {
	t.Helper()
	ctx := context.Background()
	uid, _ := setupUsers(t)
	u, err := testStore.UserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := testStore.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := testStore.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := mfa.NewCipher(strings.Repeat("ab", 32))
	pending, err := testStore.ManageMFA(ctx, uid, sess.ID, "setup", "pass123456", "", "", "", "", c)
	if err != nil {
		t.Fatal(err)
	}
	result, err := testStore.ManageMFA(ctx, uid, sess.ID, "enable", "pass123456", pending.SetupID, mfa.Code(pending.Secret, time.Now().Unix()/30), "", "", c)
	if err != nil {
		t.Fatal(err)
	}
	return mfaFixture{uid, sess.ID, token, u.PasswordHash, pending.Secret, result.RecoveryCodes, c}
}

func (f mfaFixture) challenge(t *testing.T) string {
	t.Helper()
	ch, err := testStore.CreateMFAChallenge(context.Background(), f.uid, f.hash, "browser")
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func TestMFAEnrollmentGuardsAndRotation(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	token, _, _ := testStore.CreateSession(ctx, uid)
	sess, _ := testStore.Session(ctx, token)
	other, _, _ := testStore.CreateSession(ctx, uid)
	otherSess, _ := testStore.Session(ctx, other)
	c, _ := mfa.NewCipher(strings.Repeat("ab", 32))
	manage := func(action, password, id, code, recovery string, sid int64) (MFAEnrollment, error) {
		return testStore.ManageMFA(ctx, uid, sid, action, password, id, code, recovery, "", c)
	}
	if _, err := manage("setup", "wrong", "", "", "", sess.ID); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal(err)
	}
	p, err := manage("setup", "pass123456", "", "", "", sess.ID)
	if err != nil || len(p.RecoveryCodes) != 0 {
		t.Fatal(err)
	}
	code := mfa.Code(p.Secret, time.Now().Unix()/30)
	if _, err = manage("enable", "pass123456", p.SetupID, code, "", otherSess.ID); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal("wrong session", err)
	}
	if _, err = manage("enable", "pass123456", "stale", code, "", sess.ID); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal("wrong setup", err)
	}
	enabled, err := manage("enable", "pass123456", p.SetupID, code, "", sess.ID)
	if err != nil || len(enabled.RecoveryCodes) != 10 {
		t.Fatal(err)
	}
	if _, err = testStore.Session(ctx, other); !errors.Is(err, ErrNotFound) {
		t.Fatal("other session survived enable", err)
	}
	if _, err = manage("setup", "pass123456", "", "", "", sess.ID); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal("overwrote active secret", err)
	}
	if _, _, err = testStore.CreateSession(ctx, uid); !errors.Is(err, ErrMFARequired) {
		t.Fatal("password-only bypass", err)
	}
	if _, err = manage("disable", "pass123456", "", "", "", sess.ID); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal("no second factor", err)
	}
	rotated, err := manage("recovery.rotate", "pass123456", "", "", enabled.RecoveryCodes[0], sess.ID)
	if err != nil || len(rotated.RecoveryCodes) != 10 {
		t.Fatal(err)
	}
	if _, err = manage("disable", "pass123456", "", "", enabled.RecoveryCodes[1], sess.ID); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal("old recovery survived rotation", err)
	}
	if _, err = manage("disable", "pass123456", "", "", rotated.RecoveryCodes[0], sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.CreateSession(ctx, uid); err != nil {
		t.Fatal(err)
	}
}

func TestMFAEnrollmentExpiryAndPasswordChange(t *testing.T) {
	for _, change := range []string{"expiry", "password", "session"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			uid, _ := setupUsers(t)
			raw, _, _ := testStore.CreateSession(ctx, uid)
			sess, _ := testStore.Session(ctx, raw)
			c, _ := mfa.NewCipher(strings.Repeat("ab", 32))
			p, err := testStore.ManageMFA(ctx, uid, sess.ID, "setup", "pass123456", "", "", "", "", c)
			if err != nil {
				t.Fatal(err)
			}
			password := "pass123456"
			switch change {
			case "expiry":
				_, err = testPool.Exec(ctx, `UPDATE user_mfa SET setup_expires=now()-interval '1 second' WHERE user_id=$1`, uid)
			case "password":
				err = testStore.ChangePassword(ctx, uid, password, "different-password", raw)
				password = "different-password"
			case "session":
				err = testStore.DeleteSession(ctx, raw)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = testStore.ManageMFA(ctx, uid, sess.ID, "enable", password, p.SetupID, mfa.Code(p.Secret, time.Now().Unix()/30), "", "", c); !errors.Is(err, ErrMFAInvalid) {
				t.Fatal(err)
			}
			if err = testStore.PurgeMFA(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMFAConcurrentProofsAndRollback(t *testing.T) {
	for _, kind := range []string{"totp", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := setupMFA(t)
			a, b := f.challenge(t), f.challenge(t)
			// Enrollment consumed the current step. Use the next accepted step for
			// login, keeping the same proof for rollback and replay assertions.
			code, recovery := mfa.Code(f.secret, time.Now().Unix()/30+1), ""
			if kind == "recovery" {
				code, recovery = "", f.codes[0]
			}
			// Inject a failure after proof and session writes, at the final audit insert.
			if _, err := testPool.Exec(ctx, `CREATE OR REPLACE FUNCTION test_mfa_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='mfa.login' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_mfa_fail BEFORE INSERT ON admin_logs FOR EACH ROW EXECUTE FUNCTION test_mfa_fail()`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = testPool.Exec(ctx, `DROP TRIGGER IF EXISTS test_mfa_fail ON admin_logs; DROP FUNCTION IF EXISTS test_mfa_fail()`)
			})
			if _, raw, _, err := testStore.CompleteMFA(ctx, a, "browser", code, recovery, "", "", f.cipher); err == nil || raw != "" {
				t.Fatal("injection did not fail")
			}
			var active, attempts int
			var unused bool
			if err := testPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL),used_at IS NULL,attempts FROM mfa_challenges WHERE token_hash=$2`, f.uid, hashToken(a)).Scan(&active, &unused, &attempts); err != nil || active != 1 || !unused || attempts != 0 {
				t.Fatal(active, unused, attempts, err)
			}
			if _, err := testPool.Exec(ctx, `DROP TRIGGER test_mfa_fail ON admin_logs; DROP FUNCTION test_mfa_fail()`); err != nil {
				t.Fatal(err)
			}
			var successes atomic.Int32
			var wg sync.WaitGroup
			for _, ch := range []string{a, b} {
				wg.Add(1)
				go func(ch string) {
					defer wg.Done()
					_, raw, _, err := New(testPool).CompleteMFA(ctx, ch, "browser", code, recovery, "", "", f.cipher)
					if err == nil {
						successes.Add(1)
						if _, err := testStore.Session(ctx, raw); err != nil {
							t.Error(err)
						}
					} else if !errors.Is(err, ErrMFAInvalid) {
						t.Error(err)
					}
				}(ch)
			}
			wg.Wait()
			if successes.Load() != 1 {
				t.Fatal("proof reused", successes.Load())
			}
			for _, ch := range []string{a, b} {
				if _, _, _, err := testStore.CompleteMFA(ctx, ch, "browser", code, recovery, "", "", f.cipher); !errors.Is(err, ErrMFAInvalid) {
					t.Fatal("replay", err)
				}
			}
			var leaked bool
			if err := testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_logs WHERE uid=$1 AND (detail LIKE '%'||$2||'%' OR detail LIKE '%'||$3||'%'))`, f.uid, f.secret, f.codes[0]).Scan(&leaked); err != nil || leaked {
				t.Fatal("audit leaked credential", err)
			}
		})
	}
}

func TestMFAChallengeBindingLimitsAndRestart(t *testing.T) {
	ctx := context.Background()
	f := setupMFA(t)
	ch := f.challenge(t)
	if _, _, _, err := testStore.CompleteMFA(ctx, ch, "wrong-browser", "", f.codes[0], "", "", f.cipher); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal(err)
	}
	var attempts int
	if err := testPool.QueryRow(ctx, `SELECT attempts FROM mfa_challenges WHERE token_hash=$1`, hashToken(ch)).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal(attempts, err)
	}
	for n := 0; n < 2; n++ {
		if n == 1 {
			ch = f.challenge(t)
		}
		for i := 0; i < 5; i++ {
			if _, _, _, err := New(testPool).CompleteMFA(ctx, ch, "browser", "bad", "", "", "", f.cipher); !errors.Is(err, ErrMFAInvalid) {
				t.Fatal(err)
			}
		}
		if _, _, _, err := testStore.CompleteMFA(ctx, ch, "browser", "", f.codes[0], "", "", f.cipher); !errors.Is(err, ErrMFAInvalid) {
			t.Fatal("exhausted challenge", err)
		}
	}
	ch = f.challenge(t)
	if _, _, _, err := New(testPool).CompleteMFA(ctx, ch, "browser", "", f.codes[0], "", "", f.cipher); !errors.Is(err, ErrMFALimited) {
		t.Fatal("account limit reset", err)
	}
	f.challenge(t)
	f.challenge(t)
	if _, err := New(testPool).CreateMFAChallenge(ctx, f.uid, f.hash, "browser"); !errors.Is(err, ErrMFALimited) {
		t.Fatal("challenge issue limit", err)
	}
}

func TestMFAChallengeInvalidationAndMissingKey(t *testing.T) {
	for _, action := range []string{"block", "password", "others", "all", "disable", "rotate", "expiry"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			f := setupMFA(t)
			ch := f.challenge(t)
			var err error
			if _, _, _, err = testStore.CompleteMFA(ctx, ch, "browser", "", f.codes[0], "", "", nil); !errors.Is(err, ErrMFAUnavailable) {
				t.Fatal(err)
			}
			wrong, _ := mfa.NewCipher(strings.Repeat("cd", 32))
			if _, _, _, err = testStore.CompleteMFA(ctx, ch, "browser", "", f.codes[0], "", "", wrong); !errors.Is(err, ErrMFAUnavailable) {
				t.Fatal(err)
			}
			switch action {
			case "block":
				err = testStore.BlockUser(ctx, f.uid, 0)
				if err == nil {
					err = testStore.UnblockUser(ctx, f.uid)
				}
			case "password":
				err = testStore.ChangePassword(ctx, f.uid, "pass123456", "new-password123", f.token)
			case "others", "all":
				_, err = testStore.ManageDevice(ctx, f.uid, f.sid, 0, action, "")
			case "disable":
				_, err = testStore.ManageMFA(ctx, f.uid, f.sid, "disable", "pass123456", "", "", f.codes[1], "", f.cipher)
			case "rotate":
				_, err = testStore.ManageMFA(ctx, f.uid, f.sid, "recovery.rotate", "pass123456", "", "", f.codes[1], "", f.cipher)
			case "expiry":
				_, err = testPool.Exec(ctx, `UPDATE mfa_challenges SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, hashToken(ch))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err = testStore.CompleteMFA(ctx, ch, "browser", "", f.codes[0], "", "", f.cipher); !errors.Is(err, ErrMFAInvalid) {
				t.Fatal("stale challenge", err)
			}
		})
	}
}
