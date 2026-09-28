package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"dzforum/internal/store"
)

func TestBountyFreezeAwardRefund(t *testing.T) {
	engagementAPIFixture(t)
	owner, ownerCookie, ownerCSRF := memberTestUser(t)
	recipient, err := smokeSrv.st.CreateUser(context.Background(), "bounty_recipient_"+fmt.Sprint(t.Name()), "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	th, _, err := smokeSrv.st.CreateThread(context.Background(), 1, owner.ID, owner.Username, "bounty test", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.AdjustPoints(context.Background(), owner.ID, 1, store.PointsAdjustment{Version: 1, Delta: 20, Reason: "seed", Key: "seed-0001"}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/threads/%d/bounty", th.ID)
	for i := 0; i < 2; i++ {
		checkJSON(t, memberJSON(t, "POST", path, store.BountyInput{Amount: 10, DurationHours: 24}, ownerCSRF, ownerCookie), 200)
	}
	a, err := smokeSrv.st.PointsAccount(context.Background(), owner.ID)
	if err != nil || a.Frozen != 10 || a.Available != 10 {
		t.Fatal(a, err)
	}
	_, reply, err := smokeSrv.st.CreateReply(context.Background(), th.ID, recipient.ID, recipient.Username, "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	// Fail after both ledger entries were written; acceptance and funds must roll back together.
	flowSQL(t, `CREATE FUNCTION bounty_accept_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected acceptance failure'; END $$; CREATE TRIGGER bounty_accept_fail BEFORE INSERT ON acceptance_logs FOR EACH ROW EXECUTE FUNCTION bounty_accept_fail()`)
	t.Cleanup(func() {
		flowSQL(t, `DROP TRIGGER bounty_accept_fail ON acceptance_logs; DROP FUNCTION bounty_accept_fail()`)
	})
	if err = smokeSrv.st.SetAcceptedReply(context.Background(), th.ID, reply.ID, owner.ID, true); err == nil {
		t.Fatal("injected settlement failure was ignored")
	}
	accepted, err := smokeSrv.st.AcceptedReply(context.Background(), th.ID)
	if err != nil || accepted != 0 {
		t.Fatal("acceptance leaked", accepted, err)
	}
	a, err = smokeSrv.st.PointsAccount(context.Background(), owner.ID)
	if err != nil || a.Balance != 20 || a.Frozen != 10 {
		t.Fatal("payer rollback", a, err)
	}
	a, err = smokeSrv.st.PointsAccount(context.Background(), recipient.ID)
	if err != nil || a.Balance != 0 {
		t.Fatal("payee rollback", a, err)
	}
	pending, err := smokeSrv.st.Bounty(context.Background(), th.ID)
	if err != nil || pending.State != "active" {
		t.Fatal("bounty rollback", pending, err)
	}
	flowSQL(t, `ALTER TABLE acceptance_logs DISABLE TRIGGER bounty_accept_fail`)
	for i := 0; i < 2; i++ {
		checkJSON(t, memberJSON(t, "PUT", fmt.Sprintf("/api/v1/posts/%d/acceptance", reply.ID), map[string]any{}, ownerCSRF, ownerCookie), 200)
	}
	b, err := smokeSrv.st.Bounty(context.Background(), th.ID)
	if err != nil || b.State != "awarded" || b.RecipientID != recipient.ID {
		t.Fatal(b, err)
	}
	a, err = smokeSrv.st.PointsAccount(context.Background(), owner.ID)
	if err != nil || a.Frozen != 0 || a.Balance != 10 {
		t.Fatal(a, err)
	}
	a, err = smokeSrv.st.PointsAccount(context.Background(), recipient.ID)
	if err != nil || a.Balance != 10 {
		t.Fatal(a, err)
	}
	assertEngagementPoints(t, owner.ID, 10, 0)
	assertEngagementPoints(t, recipient.ID, 10, 0)
	checkJSON(t, memberJSON(t, "DELETE", fmt.Sprintf("/api/v1/posts/%d/acceptance", reply.ID), map[string]any{}, ownerCSRF, ownerCookie), 409)
}

func TestCheckinUniqueAndTimeZoneGuard(t *testing.T) {
	c := engagementAPIFixture(t)
	u, cookie, csrf := memberTestUser(t)
	checkJSON(t, smokeGet(t, "/api/v1/me/checkin", cookie), 200)
	first := checkJSON(t, memberJSON(t, "POST", "/api/v1/me/checkin", map[string]any{}, csrf, cookie), 200)
	second := checkJSON(t, memberJSON(t, "POST", "/api/v1/me/checkin", map[string]any{}, csrf, cookie), 200)
	if string(first["data"]) != string(second["data"]) {
		t.Fatal("duplicate claim changed result")
	}
	a, err := smokeSrv.st.PointsAccount(context.Background(), u.ID)
	if err != nil || a.Balance != 1 {
		t.Fatal(a, err)
	}
	c.Checkin.TimeZone = "UTC"
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/admin/engagement/config", c, adminCSRF, adminCookie), 409)
	checkJSON(t, smokeGet(t, "/api/v1/me/checkins", cookie), 200)
}

func assertEngagementPoints(t *testing.T, uid, balance, frozen int64) {
	t.Helper()
	a, err := smokeSrv.st.PointsAccount(context.Background(), uid)
	if err != nil || a.Balance != balance || a.Frozen != frozen {
		t.Fatal(a, err)
	}
	r, err := smokeSrv.st.ReconcilePoints(context.Background(), uid)
	if err != nil || r["consistent"] != true {
		t.Fatal("ledger mismatch", r, err)
	}
}

func TestBountyCancellationAndWorkerRefund(t *testing.T) {
	engagementAPIFixture(t)
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	if err := smokeSrv.st.AdjustPoints(ctx, u.ID, 1, store.PointsAdjustment{Version: 1, Delta: 20, Reason: "seed", Key: "refund-seed"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cancel", "expired", "deleted", "admin"} {
		t.Run(mode, func(t *testing.T) {
			th, _, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "refund "+mode, "body", "", false, "")
			if err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/api/v1/threads/%d/bounty", th.ID)
			checkJSON(t, memberJSON(t, "POST", path, store.BountyInput{Amount: 10, DurationHours: 24}, csrf, cookie), 200)
			checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{}, userCSRF, userCookie), 403)
			state := "canceled"
			if mode == "cancel" {
				for i := 0; i < 2; i++ {
					checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{}, csrf, cookie), 200)
				}
			} else {
				if _, _, err = smokeSrv.st.CreateReply(ctx, th.ID, 2, "user", "reply", "", false, ""); err != nil {
					t.Fatal(err)
				}
				checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{}, csrf, cookie), 409)
				if mode == "admin" {
					adminPath := fmt.Sprintf("/api/v1/admin/bounties/%d/cancel", th.ID)
					checkJSON(t, memberJSON(t, "POST", adminPath, map[string]any{"reason": ""}, adminCSRF, adminCookie), 422)
					checkJSON(t, memberJSON(t, "POST", adminPath, map[string]any{"reason": "管理员退款"}, adminCSRF, adminCookie), 200)
				} else {
					if mode == "expired" {
						flowSQL(t, `UPDATE thread_bounties SET closes_at=now()-interval '1 second' WHERE thread_id=$1`, th.ID)
						state = "expired"
					} else {
						flowSQL(t, `UPDATE threads SET deleted=true WHERE id=$1`, th.ID)
					}
					for i := 0; i < 2; i++ {
						n, err := smokeSrv.st.ProcessBountyRefunds(ctx)
						if err != nil || n != 1-i {
							t.Fatal("refund replay", n, err)
						}
					}
				}
			}
			b, err := smokeSrv.st.Bounty(ctx, th.ID)
			if err != nil || b.State != state {
				t.Fatal(b, err)
			}
			assertEngagementPoints(t, u.ID, 20, 0)
		})
	}
}

func TestCheckinConcurrentRewardsRollbackAndStreak(t *testing.T) {
	engagementAPIFixture(t)
	ctx := context.Background()
	u, _, _ := memberTestUser(t)
	var initialXP int64
	if err := smokePool.QueryRow(ctx, `SELECT experience FROM member_states WHERE user_id=$1`, u.ID).Scan(&initialXP); err != nil {
		t.Fatal(err)
	}
	flowSQL(t, `INSERT INTO checkin_records(user_id,day,streak,experience,points,rule_version,time_zone) VALUES($1,(clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date-1,3,0,0,1,'Asia/Shanghai')`, u.ID)
	flowSQL(t, `CREATE FUNCTION checkin_reward_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='checkin' THEN RAISE EXCEPTION 'injected checkin reward failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER checkin_reward_fail BEFORE INSERT ON points_ledger FOR EACH ROW EXECUTE FUNCTION checkin_reward_fail()`)
	t.Cleanup(func() {
		flowSQL(t, `DROP TRIGGER checkin_reward_fail ON points_ledger; DROP FUNCTION checkin_reward_fail()`)
	})
	if _, err := smokeSrv.st.ClaimCheckin(ctx, u.ID); err == nil {
		t.Fatal("reward failure ignored")
	}
	status, err := smokeSrv.st.CheckinStatus(ctx, u.ID)
	if err != nil || status.CheckedIn {
		t.Fatal("failed claim persisted", status, err)
	}
	var xp int64
	if err = smokePool.QueryRow(ctx, `SELECT experience FROM member_states WHERE user_id=$1`, u.ID).Scan(&xp); err != nil || xp != initialXP {
		t.Fatal("XP rollback", xp, err)
	}
	assertEngagementPoints(t, u.ID, 0, 0)
	flowSQL(t, `ALTER TABLE points_ledger DISABLE TRIGGER checkin_reward_fail`)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := smokeSrv.st.ClaimCheckin(ctx, u.ID)
			if e == nil && (v.Streak != 4 || v.Experience != 5 || v.Points != 1) {
				e = fmt.Errorf("unexpected receipt: %+v", v)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	assertEngagementPoints(t, u.ID, 1, 0)
	if err = smokePool.QueryRow(ctx, `SELECT experience FROM member_states WHERE user_id=$1`, u.ID).Scan(&xp); err != nil || xp != initialXP+5 {
		t.Fatal("duplicate XP", xp, err)
	}
}

func TestBountyRefundFailureIsolationAndRetry(t *testing.T) {
	engagementAPIFixture(t)
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	if err := smokeSrv.st.AdjustPoints(ctx, u.ID, 1, store.PointsAdjustment{Version: 1, Delta: 30, Reason: "seed", Key: "retry-seed"}); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 2; i++ {
		th, _, err := smokeSrv.st.CreateThread(ctx, 1, u.ID, u.Username, "refund isolation", "body", "", false, "")
		if err != nil {
			t.Fatal(err)
		}
		if err = smokeSrv.st.CreateBounty(ctx, th.ID, u.ID, store.BountyInput{Amount: 10, DurationHours: 24}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	flowSQL(t, `UPDATE thread_bounties SET closes_at=now()-interval '1 second' WHERE thread_id=ANY($1)`, ids)
	flowSQL(t, fmt.Sprintf(`CREATE FUNCTION refund_fail_one() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source='bounty:%d:refund' THEN RAISE EXCEPTION 'private injected failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER refund_fail_one BEFORE INSERT ON points_ledger FOR EACH ROW EXECUTE FUNCTION refund_fail_one()`, ids[0]))
	t.Cleanup(func() { flowSQL(t, `DROP TRIGGER refund_fail_one ON points_ledger; DROP FUNCTION refund_fail_one()`) })
	n, err := smokeSrv.st.ProcessBountyRefunds(ctx)
	if err != nil || n != 1 {
		t.Fatal("later refund blocked", n, err)
	}
	b, err := smokeSrv.st.AdminBounty(ctx, ids[0])
	if err != nil || b.RefundAttempts != 1 || b.RefundErrorCode != "REFUND_TRANSACTION_FAILED" || b.RefundNextAttemptAt == nil || !b.RefundNextAttemptAt.After(time.Now()) {
		t.Fatal(b, err)
	}
	assertEngagementPoints(t, u.ID, 30, 10)
	// A second worker must honor persisted backoff instead of attempting again.
	n, err = smokeSrv.st.ProcessBountyRefunds(ctx)
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
	b, err = smokeSrv.st.AdminBounty(ctx, ids[0])
	if err != nil || b.RefundAttempts != 1 {
		t.Fatal(b, err)
	}
	p := fmt.Sprintf("/api/v1/admin/bounties/%d", ids[0])
	checkJSON(t, smokeGet(t, p, cookie), 403)
	checkJSON(t, memberJSON(t, "POST", p+"/retry", map[string]any{"reason": "retry"}, csrf, cookie), 403)
	checkJSON(t, memberJSON(t, "POST", p+"/retry", map[string]any{"reason": "retry"}, "", adminCookie), 403)
	checkJSON(t, smokeGet(t, p, adminCookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/admin/bounties/diagnostics", adminCookie), 200)
	env := checkJSON(t, smokeGet(t, "/api/v1/admin/bounties?state=all&refundFailed=true", adminCookie), 200)
	var data struct {
		Items []store.AdminBounty `json:"items"`
	}
	if err = json.Unmarshal(env["data"], &data); err != nil || len(data.Items) != 1 || data.Items[0].ThreadID != ids[0] {
		t.Fatal(data, err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/admin/bounties?state=invalid", adminCookie), 422)
	checkJSON(t, memberJSON(t, "POST", p+"/retry", map[string]any{"reason": ""}, adminCSRF, adminCookie), 422)
	flowSQL(t, `ALTER TABLE points_ledger DISABLE TRIGGER refund_fail_one`)
	for i := 0; i < 2; i++ {
		checkJSON(t, memberJSON(t, "POST", p+"/retry", map[string]any{"reason": "已排除故障"}, adminCSRF, adminCookie), 202)
	}
	n, err = smokeSrv.st.ProcessBountyRefunds(ctx)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	b, err = smokeSrv.st.AdminBounty(ctx, ids[0])
	if err != nil || b.State != "expired" || b.RefundErrorCode != "" || b.RefundNextAttemptAt != nil {
		t.Fatal(b, err)
	}
	checkJSON(t, memberJSON(t, "POST", p+"/retry", map[string]any{"reason": "重复退款"}, adminCSRF, adminCookie), 409)
	assertEngagementPoints(t, u.ID, 30, 0)
}
