package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func emailChangeFixture(t *testing.T) (int64, int64, string, string) {
	t.Helper()
	ctx := context.Background()
	uid, _ := setupUsers(t)
	old := fmt.Sprintf("old-%d@example.test", uid)
	if _, err := testPool.Exec(ctx, `UPDATE users SET email=$2,email_verified=true WHERE id=$1`, uid, old); err != nil {
		t.Fatal(err)
	}
	session, _, err := testStore.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := testStore.Session(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	return uid, sess.ID, session, old
}

func requestTestEmailChange(t *testing.T, uid, sid int64) string {
	t.Helper()
	var raw string
	err := testStore.RequestEmailChange(context.Background(), uid, sid, fmt.Sprintf("new-%d@example.test", uid), "pass123456", "", "", "", nil, func(v string) (string, error) { raw = v; return "sealed", nil })
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEmailChangeInvalidationAndRollback(t *testing.T) {
	for _, mode := range []string{"expired", "revoked", "password", "reset", "superseded", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			uid, sid, session, old := emailChangeFixture(t)
			raw := requestTestEmailChange(t, uid, sid)
			var err error
			switch mode {
			case "expired":
				_, err = testPool.Exec(ctx, `UPDATE email_changes SET expires_at=now()-interval '1 second' WHERE uid=$1`, uid)
			case "revoked":
				testStore.DeleteSession(ctx, session)
			case "password":
				err = testStore.ChangePassword(ctx, uid, "pass123456", "new-password", session)
			case "reset":
				var token string
				token, err = testStore.CreatePasswordReset(ctx, uid)
				if err == nil {
					_, err = testStore.ResetPasswordByToken(ctx, token, "new-password")
				}
			case "superseded":
				requestTestEmailChange(t, uid, sid)
			case "blocked":
				_, err = testPool.Exec(ctx, `UPDATE users SET blocked_until=now()+interval '1 day' WHERE id=$1`, uid)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = testStore.ConfirmEmailChange(ctx, uid, sid, raw, ""); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrMFAInvalid) {
				t.Fatal("stale confirmation accepted", err)
			}
			u, err := testStore.UserByID(ctx, uid)
			if err != nil || u.Email != old || !u.EmailVerified {
				t.Fatal(u, err)
			}
		})
	}
	t.Run("queue_failure", func(t *testing.T) {
		ctx := context.Background()
		uid, sid, _, old := emailChangeFixture(t)
		if _, err := testPool.Exec(ctx, `CREATE FUNCTION fail_change_mail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='email_change' THEN RAISE EXCEPTION 'queue failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_change_mail BEFORE INSERT ON email_jobs FOR EACH ROW EXECUTE FUNCTION fail_change_mail()`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := testPool.Exec(ctx, `DROP TRIGGER fail_change_mail ON email_jobs; DROP FUNCTION fail_change_mail()`); err != nil {
				t.Error(err)
			}
		})
		err := testStore.RequestEmailChange(ctx, uid, sid, "failure@example.test", "pass123456", "", "", "", nil, func(string) (string, error) { return "sealed", nil })
		if err == nil {
			t.Fatal("queue failure ignored")
		}
		var count int
		if err = testPool.QueryRow(ctx, `SELECT count(*) FROM email_changes WHERE uid=$1`, uid).Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
		u, err := testStore.UserByID(ctx, uid)
		if err != nil || u.Email != old {
			t.Fatal(u, err)
		}
	})
}

func TestEmailChangeConcurrentConfirmationAndReset(t *testing.T) {
	for _, mode := range []string{"confirmation", "reset"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			uid, sid, _, _ := emailChangeFixture(t)
			raw := requestTestEmailChange(t, uid, sid)
			reset, err := testStore.CreatePasswordReset(ctx, uid)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if i == 1 && mode == "reset" {
						_, e := testStore.ResetPasswordByToken(ctx, reset, "replacement-password")
						results <- e
						return
					}
					results <- testStore.ConfirmEmailChange(ctx, uid, sid, raw, "")
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			success := 0
			for e := range results {
				if e == nil {
					success++
				} else if !errors.Is(e, ErrNotFound) && !errors.Is(e, ErrTokenInvalid) {
					t.Fatal(e)
				}
			}
			if success != 1 {
				t.Fatal("exactly one account transition must succeed", success)
			}
		})
	}
}

func TestEmailChangeRevalidatesSessionAndLimitsProofs(t *testing.T) {
	ctx := context.Background()
	uid, sid, session, _ := emailChangeFixture(t)
	for i := 0; i < 6; i++ {
		err := testStore.RequestEmailChange(ctx, uid, sid, "limit@example.test", "wrong", "", "", "", nil, nil)
		want := ErrMFAInvalid
		if i == 5 {
			want = ErrMFALimited
		}
		if !errors.Is(err, want) {
			t.Fatal(i, err)
		}
	}
	testStore.DeleteSession(ctx, session)
	if err := testStore.RequestEmailChange(ctx, uid, sid, "limit@example.test", "pass123456", "", "", "", nil, nil); !errors.Is(err, ErrMFAInvalid) {
		t.Fatal(err)
	}
}

func TestEmailChangeConfirmationRollbackAndAddressConflict(t *testing.T) {
	ctx := context.Background()
	uid, sid, _, old := emailChangeFixture(t)
	raw := requestTestEmailChange(t, uid, sid)
	reset, err := testStore.CreatePasswordReset(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `CREATE FUNCTION fail_change_notice() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='email_changed' THEN RAISE EXCEPTION 'notice failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_change_notice BEFORE INSERT ON email_jobs FOR EACH ROW EXECUTE FUNCTION fail_change_notice()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `DROP TRIGGER fail_change_notice ON email_jobs; DROP FUNCTION fail_change_notice()`); err != nil {
			t.Error(err)
		}
	})
	if err = testStore.ConfirmEmailChange(ctx, uid, sid, raw, ""); err == nil {
		t.Fatal("notice failure ignored")
	}
	u, err := testStore.UserByID(ctx, uid)
	if err != nil || u.Email != old {
		t.Fatal(u, err)
	}
	if _, err = testStore.ResetUIDByToken(ctx, reset); err != nil {
		t.Fatal("rollback invalidated old reset", err)
	}
	if _, err = testPool.Exec(ctx, `ALTER TABLE email_jobs DISABLE TRIGGER fail_change_notice`); err != nil {
		t.Fatal(err)
	}
	other, err := testStore.CreateUser(ctx, "conflict"+t.Name(), "pass123456", fmt.Sprintf("new-%d@example.test", uid))
	if err != nil {
		t.Fatal(err)
	}
	if err = testStore.ConfirmEmailChange(ctx, uid, sid, raw, ""); !errors.Is(err, ErrEmailUnavailable) {
		t.Fatal("occupied address accepted", err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE users SET email='' WHERE id=$1`, other.ID); err != nil {
		t.Fatal(err)
	}
	if err = testStore.ConfirmEmailChange(ctx, uid, sid, raw, ""); err != nil {
		t.Fatal("failed confirmation could not retry", err)
	}
	var token string
	if err = testStore.QueueAuthEmail(ctx, uid, fmt.Sprintf("new-%d@example.test", uid), "password_reset", func(v string) (string, error) { token = v; return "sealed", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.ResetPasswordByToken(ctx, token, "new-password"); err != nil {
		t.Fatal("new recovery address failed", err)
	}
}
