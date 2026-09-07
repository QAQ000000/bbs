package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"dzforum/internal/mail"
)

func emailUser(t *testing.T) (int64, *mail.TokenCipher) {
	t.Helper()
	u, err := testStore.CreateUser(context.Background(), "mail_"+t.Name(), "password123", t.Name()+"@example.test")
	if err != nil {
		t.Fatal(err)
	}
	c, err := mail.NewTokenCipher(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	return u.ID, c
}

func TestEmailQueueAuthAtomicityAndPrivacy(t *testing.T) {
	ctx := context.Background()
	uid, c := emailUser(t)
	email := t.Name() + "@example.test"
	if err := testStore.QueueAuthEmail(ctx, uid, email, "password_reset", c.Seal); err != nil {
		t.Fatal(err)
	}
	j, err := scanEmail(testPool.QueryRow(ctx, `SELECT `+emailCols+` FROM email_jobs WHERE uid=$1`, uid))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.Open(j.SealedToken)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := testStore.ResetUIDByToken(ctx, raw); err != nil || got != uid {
		t.Fatal(got, err)
	}
	b, _ := json.Marshal(j)
	if strings.Contains(string(b), email) || strings.Contains(string(b), raw) || strings.Contains(string(b), j.SealedToken) || strings.Contains(string(b), j.TokenHash) {
		t.Fatal("admin JSON exposed secret")
	}
	// Fail after token insertion, when the outbox transaction writes its job.
	if _, err = testPool.Exec(ctx, `CREATE FUNCTION test_reject_email_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected enqueue failure'; END $$;
 CREATE TRIGGER test_reject_email BEFORE INSERT ON email_jobs FOR EACH ROW EXECUTE FUNCTION test_reject_email_job()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DROP TRIGGER IF EXISTS test_reject_email ON email_jobs;DROP FUNCTION IF EXISTS test_reject_email_job()`)
	})
	var attempted string
	err = testStore.QueueAuthEmail(ctx, uid, email, "password_reset", func(raw string) (string, error) { attempted = raw; return c.Seal(raw) })
	if err == nil {
		t.Fatal("injected failure ignored")
	}
	if _, err = testStore.ResetUIDByToken(ctx, attempted); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed enqueue left usable token", err)
	}
	if _, err = testStore.CreateUserWithVerification(ctx, "outbox_registration_failure", "password123", "register@example.test", c.Seal); err == nil {
		t.Fatal("registration ignored failed enqueue")
	}
	if _, err = testStore.UserByName(ctx, "outbox_registration_failure"); !errors.Is(err, ErrNotFound) {
		t.Fatal("registration survived without email", err)
	}
}

func TestEmailQueueConcurrentClaimsLeaseAndRetry(t *testing.T) {
	ctx := context.Background()
	uid, c := emailUser(t)
	// Isolate claims from jobs made by other tests; no running worker uses this database.
	if _, err := testPool.Exec(ctx, `DELETE FROM email_jobs`); err != nil {
		t.Fatal(err)
	}
	if err := testStore.QueueAuthEmail(ctx, uid, t.Name()+"@example.test", "email_verify", c.Seal); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan *EmailJob, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := testStore.ClaimEmail(ctx)
			if errors.Is(e, ErrNotFound) {
				return
			}
			if e != nil {
				errs <- e
				return
			}
			results <- j
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatal("duplicate claim", len(results))
	}
	first := <-results
	if _, err := testPool.Exec(ctx, `UPDATE email_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	restarted := New(testPool)
	second, err := restarted.ClaimEmail(ctx)
	if err != nil || second.ID != first.ID || second.Attempts != 2 {
		t.Fatal(second, err)
	}
	if err = testStore.FinishEmail(ctx, first, "sent", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale lease accepted", err)
	}
	if err = restarted.FinishEmail(ctx, second, "dead", "smtp_550"); err != nil {
		t.Fatal(err)
	}
	if err = restarted.RetryEmail(ctx, second.ID, first.Version, uid); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale admin version accepted")
	}
	if err = restarted.RetryEmail(ctx, second.ID, second.Version, uid); err != nil {
		t.Fatal(err)
	}
	if err = restarted.RetryEmail(ctx, second.ID, second.Version, uid); !errors.Is(err, ErrNotFound) {
		t.Fatal("repeat retry accepted")
	}
	var audit int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM admin_logs WHERE uid=$1 AND action='email.retry'`, uid).Scan(&audit); err != nil || audit != 1 {
		t.Fatal(audit, err)
	}
	third, err := restarted.ClaimEmail(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.FinishEmail(ctx, third, "pending", "smtp_451"); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.ClaimEmail(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal("backoff ignored", err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE email_jobs SET attempts=7,next_attempt_at=now() WHERE id=$1`, third.ID); err != nil {
		t.Fatal(err)
	}
	last, err := restarted.ClaimEmail(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.FinishEmail(ctx, last, "pending", "smtp_451"); err != nil {
		t.Fatal(err)
	}
	done, err := scanEmail(testPool.QueryRow(ctx, `SELECT `+emailCols+` FROM email_jobs WHERE id=$1`, last.ID))
	if err != nil || done.Status != "dead" {
		t.Fatal(done, err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE email_jobs SET expires_at=now()-interval '1 second' WHERE id=$1`, last.ID); err != nil {
		t.Fatal(err)
	}
	if err = restarted.MaintainEmails(ctx); err != nil {
		t.Fatal(err)
	}
	done, err = scanEmail(testPool.QueryRow(ctx, `SELECT `+emailCols+` FROM email_jobs WHERE id=$1`, last.ID))
	if err != nil || done.Status != "cancelled" || done.SealedToken != "" {
		t.Fatal(done, err)
	}
}

func TestEmailQueueNotificationAtomicityAndDedup(t *testing.T) {
	ctx := context.Background()
	uid, _ := emailUser(t)
	author, _ := setupUsers(t)
	fid := setupForum(t)
	th, p, err := testStore.CreateThread(ctx, fid, author, "author", "outbox topic", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	n := &Notification{UID: uid, FromUID: author, FromName: "author", Type: "mention", ThreadID: th.ID, PostID: p.ID, EventKey: "post:outbox", Email: true}
	for i := 0; i < 2; i++ {
		if err = testStore.AddNotifications(ctx, []*Notification{n}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM email_jobs WHERE uid=$1 AND post_id=$2`, uid, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("dedup failed", count, err)
	}
	// An unsupported mail kind forces the notification and email insertion to roll back together.
	n.Type = "title.earned"
	n.EventKey = "email-test-fail"
	if err = testStore.AddNotifications(ctx, []*Notification{n}); err == nil {
		t.Fatal("expected mail constraint error")
	}
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND event_key='email-test-fail'`, uid).Scan(&count); err != nil || count != 0 {
		t.Fatal("notification committed without job", count, err)
	}
}
