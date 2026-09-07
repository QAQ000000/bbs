package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dzforum/internal/store"
)

func flowSQL(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := smokePool.Exec(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestFlowReviewReportDeleteAtomic(t *testing.T) {
	ctx := context.Background()
	u, _, _ := memberTestUser(t)
	_, p, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "report atomicity", "reported content", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.CreateReport(ctx, p.ID, 1, "review test"); err != nil {
		t.Fatal(err)
	}
	var rid int64
	if err = smokePool.QueryRow(ctx, `SELECT id FROM reports WHERE post_id=$1`, p.ID).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	flowSQL(t, `CREATE FUNCTION flow_report_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.deleted AND NOT OLD.deleted THEN RAISE EXCEPTION 'injected delete failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER flow_report_fail BEFORE UPDATE ON posts FOR EACH ROW EXECUTE FUNCTION flow_report_fail()`)
	t.Cleanup(func() { flowSQL(t, `DROP TRIGGER flow_report_fail ON posts; DROP FUNCTION flow_report_fail()`) })
	rec := memberJSON(t, "POST", "/api/v1/admin/report/handle", map[string]any{"id": fmt.Sprint(rid), "op": "delete"}, adminCSRF, adminCookie)
	var status string
	var deleted bool
	if err = smokePool.QueryRow(ctx, `SELECT r.status,p.deleted FROM reports r JOIN posts p ON p.id=r.post_id WHERE r.id=$1`, rid).Scan(&status, &deleted); err != nil {
		t.Fatal(err)
	}
	if status != "open" || rec.Code != 500 || deleted {
		t.Errorf("REPRO: injected delete failure: HTTP=%d report=%s post.deleted=%v", rec.Code, status, deleted)
	}
	var notifications, audits int
	if err = smokePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE payload->>'reportId'=$1 AND type='report.resolved'),(SELECT count(*) FROM admin_logs WHERE action='report.delete' AND detail LIKE $2)`, fmt.Sprint(rid), fmt.Sprintf("report=%d %%", rid)).Scan(&notifications, &audits); err != nil {
		t.Fatal(err)
	}
	if notifications != 0 || audits != 0 {
		t.Fatal("failed report leaked result", notifications, audits)
	}
	flowSQL(t, `ALTER TABLE posts DISABLE TRIGGER flow_report_fail`)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/report/handle", map[string]any{"id": fmt.Sprint(rid), "op": "typo"}, adminCSRF, adminCookie), 422)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/report/handle", map[string]any{"id": fmt.Sprint(rid), "op": "delete"}, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/report/handle", map[string]any{"id": fmt.Sprint(rid), "op": "delete"}, adminCSRF, adminCookie), 404)
}

func TestFlowReviewEditModerationAtomic(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	_, p, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "before edit", "safe original body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	flowSQL(t, `CREATE FUNCTION flow_pending_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.pending AND NOT OLD.pending THEN RAISE EXCEPTION 'injected moderation failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER flow_pending_fail BEFORE UPDATE ON posts FOR EACH ROW EXECUTE FUNCTION flow_pending_fail()`)
	t.Cleanup(func() { flowSQL(t, `DROP TRIGGER flow_pending_fail ON posts; DROP FUNCTION flow_pending_fail()`) })
	body := "new content https://example.test/requires-review"
	rec := memberJSON(t, "PATCH", fmt.Sprintf("/api/v1/posts/%d", p.ID), map[string]any{"version": p.Version, "subject": "after edit", "content": body}, csrf, cookie)
	var actual string
	var pending bool
	if err = smokePool.QueryRow(ctx, `SELECT content_md,pending FROM posts WHERE id=$1`, p.ID).Scan(&actual, &pending); err != nil {
		t.Fatal(err)
	}
	public := smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", p.ID), nil)
	if actual != p.ContentMD || pending || rec.Code != 500 || public.Code != 200 {
		t.Errorf("REPRO: editing returned HTTP=%d but unreviewed replacement is publicly readable (GET=%d)", rec.Code, public.Code)
	}
	var version, history int
	var title string
	if err = smokePool.QueryRow(ctx, `SELECT p.version,t.title,(SELECT count(*) FROM post_edits WHERE post_id=p.id) FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=$1`, p.ID).Scan(&version, &title, &history); err != nil {
		t.Fatal(err)
	}
	if version != p.Version || title != "before edit" || history != 0 {
		t.Fatal("partial edit committed", version, title, history)
	}
	flowSQL(t, `ALTER TABLE posts DISABLE TRIGGER flow_pending_fail`)
	checkJSON(t, memberJSON(t, "PATCH", fmt.Sprintf("/api/v1/posts/%d", p.ID), map[string]any{"version": p.Version, "subject": "after edit", "content": body}, csrf, cookie), 200)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", p.ID), nil), 404)
}

func TestFlowReviewSubscriptionAfterReapproval(t *testing.T) {
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/forums/1/subscribe", map[string]any{}, csrf, cookie), 200)
	t.Cleanup(func() {
		if err := smokeSrv.st.DeleteSubscription(ctx, u.ID, 1, "forum"); err != nil {
			t.Error(err)
		}
	})
	th, p, err := smokeSrv.st.CreateThread(ctx, 1, 1, "admin", "reapproval delivery", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.SetPostPendingModeration(ctx, p.ID, "test hide before delivery"); err != nil {
		t.Fatal(err)
	}
	later, err := smokeSrv.st.CreateUser(ctx, "later_"+t.Name(), "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	lraw, lcsrf, err := smokeSrv.st.CreateSession(ctx, later.ID)
	if err != nil {
		t.Fatal(err)
	}
	lc := &http.Cookie{Name: cookieSession, Value: lraw}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/forums/1/subscribe", map[string]any{}, lcsrf, lc), 200)
	t.Cleanup(func() { _ = smokeSrv.st.DeleteSubscription(ctx, later.ID, 1, "forum") })
	_, next, err := smokeSrv.st.CreateThread(ctx, 1, 1, "admin", "later public event", "public body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	drain := func() {
		for i := 0; i < 300; i++ {
			n, e := smokeSrv.ProcessSubscriptions(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if n == 0 {
				return
			}
		}
		t.Fatal("subscription queue did not drain")
	}
	drain()
	var delivered int
	if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2 AND type='subscription'`, u.ID, next.ID).Scan(&delivered); err != nil || delivered != 1 {
		t.Fatal("hidden event starved public event", delivered, err)
	}
	if err = smokeSrv.st.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	drain()
	var count int
	var completed bool
	if err = smokePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2 AND type='subscription'),completed FROM subscription_events WHERE post_id=$2`, u.ID, p.ID).Scan(&count, &completed); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("REPRO: subscribed before creation, no prior delivery; reapproved post has notifications=%d completed=%v", count, completed)
	}
	if err = smokeSrv.st.SetPostPendingModeration(ctx, p.ID, "repeat"); err != nil {
		t.Fatal(err)
	}
	drain()
	if err = smokeSrv.st.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	drain()
	if err = smokePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$3),(SELECT count(*) FROM notifications WHERE uid=$2 AND post_id=$3)`, u.ID, later.ID, p.ID).Scan(&count, &delivered); err != nil || count != 1 || delivered != 0 {
		t.Fatal("duplicate or retroactive notification", count, delivered, err)
	}
}

func TestFlowReviewDraftAcceptsValidPost(t *testing.T) {
	_, cookie, csrf := memberTestUser(t)
	body := strings.Repeat("\u4e2d", 15000)
	if msg := validateContent("valid long post", body, true); msg != "" {
		t.Fatal(msg)
	}
	res := memberJSON(t, "POST", "/api/v1/me/draft", map[string]any{"context": "new:1", "subject": "valid long post", "content": body}, csrf, cookie)
	if res.Code != 200 {
		t.Errorf("REPRO: valid 15000-character post cannot be autosaved: HTTP=%d bytes=%d", res.Code, len(body))
	}
	for _, n := range []int{30000, 30001} {
		status := 200
		if n > maxContentRunes {
			status = 413
		}
		checkJSON(t, memberJSON(t, "POST", "/api/v1/me/draft", map[string]any{"context": "new:1", "content": strings.Repeat("中", n)}, csrf, cookie), status)
	}
}

func TestFlowReviewApprovalRacingPendingEdit(t *testing.T) {
	u, cookie, csrf := memberTestUser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	th, p, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "pending original", "original", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := smokePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var blocker int
	if err = tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM threads WHERE id=$1 FOR UPDATE`, th.ID).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"version": {fmt.Sprint(p.Version)}, "subject": {"edited title"}, "content": {"unreviewed https://example.test"}}
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/posts/%d", p.ID), strings.NewReader(form.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	handler := smokeSrv.Handler()
	go func() { defer close(done); handler.ServeHTTP(rec, req) }()
	// Wait until the handler has read the old pending state and reached the write lock.
	for {
		var waiting bool
		if err = smokePool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-done:
			t.Fatal("edit did not reach expected lock")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE threads SET pending=false WHERE id=$1`, th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE posts SET pending=false WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET post_count=post_count+1 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	checkJSON(t, rec, 200)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", p.ID), nil), 404)
}

func TestFlowReviewHappyPathAcrossModules(t *testing.T) {
	ctx := context.Background()
	// Registration and CSRF are exercised through the same full API handler.
	register := func(name, nonce string) (*store.User, *http.Cookie, string) {
		guest := &http.Cookie{Name: cookieCSRF, Value: strings.Repeat(nonce, 32)}
		rec := memberJSON(t, "POST", "/api/v1/auth/register", map[string]any{"username": name, "password": "flow-password-123", "consent": true}, guest.Value, guest)
		checkJSON(t, rec, 200)
		var cookie *http.Cookie
		for _, c := range rec.Result().Cookies() {
			if c.Name == cookieSession {
				cookie = c
			}
		}
		if cookie == nil {
			t.Fatal("registration has no session")
		}
		sess, err := smokeSrv.st.Session(ctx, cookie.Value)
		if err != nil {
			t.Fatal(err)
		}
		u, err := smokeSrv.st.UserByID(ctx, sess.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return u, cookie, sess.CSRF
	}
	owner, oc, ocs := register("flowowner", "o")
	reply, rc, rcs := register("flowreply", "p")
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/users/%d/follow", owner.ID), map[string]any{}, rcs, rc), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/forums/1/subscribe", map[string]any{}, rcs, rc), 200)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/threads", map[string]any{"forumId": "1", "subject": "full flow", "content": "hello https://example.test"}, ocs, oc), 201)
	var tid, pid int64
	if err := smokePool.QueryRow(ctx, `SELECT id,first_post_id FROM threads WHERE author_id=$1 ORDER BY id DESC LIMIT 1`, owner.ID).Scan(&tid, &pid); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", pid), nil), 404)
	checkJSON(t, memberJSON(t, "POST", "/api/v1/admin/moderate/thread", map[string]any{"tid": fmt.Sprint(tid), "op": "approve"}, adminCSRF, adminCookie), 200)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/posts/%d", pid), nil), 200)
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/threads/%d/posts", tid), map[string]any{"content": "helpful answer"}, rcs, rc), 201)
	var answer int64
	if err := smokePool.QueryRow(ctx, `SELECT id FROM posts WHERE thread_id=$1 AND author_id=$2`, tid, reply.ID).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/posts/%d/like", answer), map[string]any{}, ocs, oc), 200)
	checkJSON(t, memberJSON(t, "PUT", fmt.Sprintf("/api/v1/posts/%d/acceptance", answer), map[string]any{}, ocs, oc), 200)
	for i := 0; i < 300; i++ {
		n, e := smokeSrv.st.ProcessMemberEvents(ctx, 100)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
	}
	points, err := smokeSrv.st.PointsAccount(ctx, reply.ID)
	if err != nil || points.Balance != 12 {
		t.Fatalf("reply+like+accepted points=%d err=%v", points.Balance, err)
	}
	reconciled, err := smokeSrv.st.ReconcilePoints(ctx, reply.ID)
	if err != nil || reconciled["consistent"] != true {
		t.Fatalf("reconciliation: %v %v", reconciled, err)
	}
	path := fmt.Sprintf("/api/v1/users/%d/messages", owner.ID)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"body": "first message"}, rcs, rc), 201)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"body": "second before response"}, rcs, rc), 409)
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/users/%d/messages", reply.ID), map[string]any{"body": "response"}, ocs, oc), 201)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"body": "allowed after response"}, rcs, rc), 201)
	t.Log("PASS: registration -> pending topic -> approval -> reply -> like -> acceptance -> points -> first-message/reply restriction")
}
