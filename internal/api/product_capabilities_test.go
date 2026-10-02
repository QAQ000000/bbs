package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func TestPopularVisibilityAndModeration(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	u, _, _ := memberTestUser(t)
	th, _, err := smokeSrv.st.CreateThread(ctx, smokeFid2, u.ID, u.Username, "popular-visible", "body", "body", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = smokeSrv.st.CreateReply(ctx, th.ID, u.ID, u.Username, "pending", "pending", true, "test"); err != nil {
		t.Fatal(err)
	}
	visible, err := smokeSrv.st.Popular(store.WithVisibleForums(ctx, []int64{smokeFid2}), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range visible.Threads {
		if item.ID == th.ID {
			found = true
			if item.Replies != 0 {
				t.Fatal("pending reply counted", item)
			}
		}
	}
	if !found {
		t.Fatal("visible topic missing", visible)
	}
	empty, err := smokeSrv.st.Popular(store.WithVisibleForums(ctx, nil), time.Now())
	if err != nil || len(empty.Threads) != 0 || len(empty.Authors) != 0 {
		t.Fatal("restricted content leaked", empty, err)
	}
	flowSQL(t, fmt.Sprintf(`UPDATE users SET blocked_until=now()+interval '1 day' WHERE id=%d`, u.ID))
	t.Cleanup(func() { flowSQL(t, fmt.Sprintf(`UPDATE users SET blocked_until=NULL WHERE id=%d`, u.ID)) })
	blocked, err := smokeSrv.st.Popular(store.WithVisibleForums(ctx, []int64{smokeFid2}), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range blocked.Threads {
		if item.ID == th.ID {
			t.Fatal("blocked author topic leaked")
		}
	}
	for _, item := range blocked.Authors {
		if item.UserID == u.ID {
			t.Fatal("blocked author leaked")
		}
	}
	checkJSON(t, smokeGet(t, "/api/v1/home/popular", nil), 200)
}

func TestPeriodLeaderboardNetChangesAndHistory(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	u, _, _ := memberTestUser(t)
	now := time.Now()
	start, end := store.PointsPeriodBounds("day", now)
	for i, event := range []struct {
		delta int64
		at    time.Time
	}{{10, start}, {-3, start}, {4, start.Add(-time.Second)}, {100, end}} {
		_, err := smokePool.Exec(ctx, `INSERT INTO points_ledger(user_id,source,kind,delta,balance_after,frozen_after,rule_version,reason,event_at) VALUES($1,$2,'admin',$3,0,0,1,'test',$4)`, u.ID, fmt.Sprintf("period-test:%d", i), event.delta, event.at)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, period := range []string{"day", "week", "month"} {
		if err := smokeSrv.st.RefreshPeriodLeaderboard(ctx, period, now); err != nil {
			t.Fatal(err)
		}
		checkJSON(t, smokeGet(t, "/api/v1/leaderboard/points?period="+period, nil), 200)
	}
	read := func(path string) map[string]any {
		return mfaAPIData(t, smokeGet(t, path, nil), 200)
	}
	board := read("/api/v1/leaderboard/points?period=day")
	found := false
	for _, raw := range board["entries"].([]any) {
		item := raw.(map[string]any)
		if item["userId"] == fmt.Sprint(u.ID) {
			found = true
			if item["points"] != float64(7) {
				t.Fatal("incorrect net change", item)
			}
		}
	}
	if !found {
		t.Fatal("period user missing", board)
	}
	date := start.Add(-time.Second).Format("2006-01-02")
	previous := read("/api/v1/leaderboard/points?period=day&date=" + date)
	if previous["complete"] != true {
		t.Fatal("previous period not finalized", previous)
	}
	checkJSON(t, smokeGet(t, "/api/v1/leaderboard/points?period=invalid", nil), 422)
	checkJSON(t, smokeGet(t, "/api/v1/leaderboard/points?period=day&date=2099-01-01", nil), 422)
	checkJSON(t, smokeGet(t, "/api/v1/leaderboard/points?period=day&date=2026-02-30", nil), 422)
	checkJSON(t, smokeGet(t, "/api/v1/leaderboard/points?period=balance&date="+date, nil), 422)
	if read("/api/v1/leaderboard/points?period=day&date=2001-01-01")["status"] != "unavailable" {
		t.Fatal("missing history not explicit")
	}
	if err := smokeSrv.st.SaveAnalyticsSnapshot(ctx, "points.day", start, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/leaderboard/points?period=day", nil), 503)
	if err := smokeSrv.st.RefreshPeriodLeaderboard(ctx, "day", now); err != nil {
		t.Fatal(err)
	}
	flowSQL(t, fmt.Sprintf(`UPDATE users SET blocked_until=now()+interval '1 day' WHERE id=%d`, u.ID))
	t.Cleanup(func() { flowSQL(t, fmt.Sprintf(`UPDATE users SET blocked_until=NULL WHERE id=%d`, u.ID)) })
	for _, raw := range read("/api/v1/leaderboard/points?period=day")["entries"].([]any) {
		if raw.(map[string]any)["userId"] == fmt.Sprint(u.ID) {
			t.Fatal("snapshot leaked blocked user")
		}
	}
}

func TestEmailDetailCancellationPermissionsAndConflict(t *testing.T) {
	requireDB(t)
	srv, _ := emailAPIServer(t)
	original := smokeSrv
	smokeSrv = srv
	t.Cleanup(func() { smokeSrv = original })
	u, _, _ := memberTestUser(t)
	email := "cancel@example.test"
	flowSQL(t, fmt.Sprintf(`UPDATE users SET email='%s' WHERE id=%d`, email, u.ID))
	if err := srv.st.QueueAuthEmail(context.Background(), u.ID, email, "password_reset", srv.mailTokens.Seal); err != nil {
		t.Fatal(err)
	}
	j := emailAPIJob(t, u.ID)
	path := fmt.Sprintf("/api/v1/admin/email-jobs/%d", j.ID)
	checkJSON(t, smokeGet(t, path, nil), 401)
	checkJSON(t, smokeGet(t, path, userCookie), 403)
	body := smokeGet(t, path, adminCookie)
	checkJSON(t, body, 200)
	for _, secret := range []string{email, j.SealedToken, j.TokenHash} {
		if secret != "" && strings.Contains(body.Body.String(), secret) {
			t.Fatal("private email payload disclosed")
		}
	}
	checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{"version": j.Version}, "", adminCookie), 403)
	checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{"version": j.Version}, userCSRF, userCookie), 403)
	matrix := perm.Matrix()
	t.Cleanup(func() { perm.Load(matrix) })
	changed := perm.Matrix()
	changed[perm.RoleAdmin][perm.EmailManage] = false
	perm.Load(changed)
	checkJSON(t, smokeGet(t, path, adminCookie), 403)
	checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{"version": j.Version}, adminCSRF, adminCookie), 403)
	perm.Load(matrix)
	checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{"version": j.Version}, adminCSRF, adminCookie), 200)
	updated := emailAPIJob(t, u.ID)
	if updated.Status != "cancelled" || updated.SealedToken != "" || updated.Version != j.Version+1 {
		t.Fatal(updated)
	}
	checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{"version": j.Version}, adminCSRF, adminCookie), 409)
	checkJSON(t, memberJSON(t, "POST", path+"/retry", map[string]any{"version": updated.Version}, adminCSRF, adminCookie), 409)
	var audit int
	if err := smokePool.QueryRow(context.Background(), `SELECT count(*) FROM admin_logs WHERE action='email.cancel' AND detail=$1`, fmt.Sprintf("email_job=%d", j.ID)).Scan(&audit); err != nil || audit != 1 {
		t.Fatal(audit, err)
	}
	for _, status := range []string{"sending", "sent"} {
		flowSQL(t, fmt.Sprintf(`UPDATE email_jobs SET status='%s' WHERE id=%d`, status, j.ID))
		checkJSON(t, memberJSON(t, "POST", path+"/cancel", map[string]any{"version": updated.Version}, adminCSRF, adminCookie), 409)
	}
	checkJSON(t, smokeGet(t, "/api/v1/admin/email-jobs/999999999", adminCookie), 404)
}

func TestSetupReadinessMetadata(t *testing.T) {
	requireDB(t)
	body := smokeGet(t, "/api/v1/setup", nil)
	checkJSON(t, body, 200)
	var envelope struct {
		Data struct {
			Required      bool
			Database      string
			Schema        int
			SMTPEnabled   bool
			SecureCookies bool
		}
	}
	if err := json.Unmarshal(body.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Required || envelope.Data.Database != "up" || envelope.Data.Schema < 21 {
		t.Fatal(envelope)
	}
}

func TestPeriodLeaderboardSnapshotRetention(t *testing.T) {
	settingsAPIFixture(t)
	ctx := context.Background()
	flowSQL(t, `INSERT INTO settings(key,value) VALUES('analytics_retention_days','35') ON CONFLICT(key) DO UPDATE SET value='35'`)
	now := time.Now()
	start, _ := store.PointsPeriodBounds("month", now)
	previous, _ := store.PointsPeriodBounds("month", start.Add(-time.Second))
	ancient, _ := store.PointsPeriodBounds("month", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := smokeSrv.st.RefreshPeriodLeaderboard(ctx, "month", now); err != nil {
		t.Fatal(err)
	}
	if err := smokeSrv.st.SaveAnalyticsSnapshot(ctx, "points.month", ancient, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := smokeSrv.st.PruneAnalyticsSnapshots(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := smokeSrv.st.PeriodLeaderboardSnapshot(ctx, "month", previous); err != nil {
		t.Fatal("previous month removed before retention elapsed", err)
	}
	if _, err := smokeSrv.st.PeriodLeaderboardSnapshot(ctx, "month", ancient); err != store.ErrNotFound {
		t.Fatal("expired month retained", err)
	}
}
