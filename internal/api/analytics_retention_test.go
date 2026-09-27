package api

import (
	"context"
	"testing"
	"time"
)

func TestAnalyticsRetentionPolicyAndBoundedCleanup(t *testing.T) {
	original := settingsAPIFixture(t)
	if original.AnalyticsRetentionDays != 30 {
		t.Fatal("unexpected default", original.AnalyticsRetentionDays)
	}
	ctx := context.Background()
	flowSQL(t, `DELETE FROM analytics_snapshots WHERE name IN ('points','site','retention-test-custom')`)
	t.Cleanup(func() {
		flowSQL(t, `DELETE FROM analytics_snapshots WHERE name IN ('points','site','retention-test-custom')`)
	})
	flowSQL(t, `INSERT INTO analytics_snapshots(name,period_start,payload)
		SELECT 'points',now()-interval '400 days'-g*interval '1 hour','[]'::jsonb FROM generate_series(0,501) g;
		INSERT INTO analytics_snapshots(name,period_start,payload) VALUES
		('site',now()-interval '400 days','{}'),('retention-test-custom',now()-interval '900 days','{}')`)
	latest, err := smokeSrv.st.LatestAnalyticsSnapshot(ctx, "points")
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := smokePool.QueryRow(ctx, `SELECT count(*) FROM analytics_snapshots WHERE name IN ('points','site','retention-test-custom')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	prune := func(want int64) {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if n, err := smokeSrv.st.PruneAnalyticsSnapshots(bounded); err != nil || n != want {
			t.Fatalf("deleted=%d want=%d err=%v", n, want, err)
		}
	}
	setDays := func(days any, status int) {
		t.Helper()
		v := settingsAPIGet(t)
		checkJSON(t, memberJSON(t, "PATCH", "/api/v1/admin/settings", map[string]any{"version": v.Version, "analyticsRetentionDays": days}, adminCSRF, adminCookie), status)
	}
	for _, invalid := range []any{-1, 3651, 1.5, "30"} {
		setDays(invalid, 422)
	}
	if settingsAPIGet(t).AnalyticsRetentionDays != 30 {
		t.Fatal("invalid input changed policy")
	}
	setDays(0, 200)
	prune(0)
	if count() != 504 {
		t.Fatal("disabled retention deleted snapshots")
	}
	setDays(30, 200)
	prune(500)
	prune(1)
	prune(0)
	after, err := smokeSrv.st.LatestAnalyticsSnapshot(ctx, "points")
	if err != nil || !after.PeriodStart.Equal(latest.PeriodStart) || count() != 3 {
		t.Fatal("latest or custom snapshot lost", after, err)
	}
	// A newer bucket makes the old last-good bucket eligible; recent buckets survive.
	flowSQL(t, `INSERT INTO analytics_snapshots(name,period_start,payload) VALUES
		('points',now()-interval '29 days','[]'),('points',now()-interval '31 days','[]')`)
	prune(2)
	if count() != 3 {
		t.Fatal("retention boundary incorrect")
	}
	// A refresh holding a row must not stall cleanup of other old buckets.
	flowSQL(t, `INSERT INTO analytics_snapshots(name,period_start,payload) VALUES
		('points',now()-interval '50 days','[]'),('points',now()-interval '40 days','[]')`)
	tx, err := smokePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT 1 FROM analytics_snapshots WHERE name='points' AND period_start<now()-interval '45 days' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	prune(1)
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// An invalid saved policy must stop deletion instead of falling back to 30 days.
	flowSQL(t, `UPDATE settings SET value='invalid' WHERE key='analytics_retention_days'`)
	if _, err = smokeSrv.st.PruneAnalyticsSnapshots(ctx); err == nil || count() != 4 {
		t.Fatal("invalid configuration allowed pruning", err)
	}
	flowSQL(t, `UPDATE settings SET value='30' WHERE key='analytics_retention_days'`)
	flowSQL(t, `CREATE FUNCTION analytics_prune_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected prune failure'; END $$; CREATE TRIGGER analytics_prune_fail BEFORE DELETE ON analytics_snapshots FOR EACH ROW EXECUTE FUNCTION analytics_prune_fail()`)
	t.Cleanup(func() {
		flowSQL(t, `DROP TRIGGER analytics_prune_fail ON analytics_snapshots; DROP FUNCTION analytics_prune_fail()`)
	})
	if _, err = smokeSrv.st.PruneAnalyticsSnapshots(ctx); err == nil || count() != 4 {
		t.Fatal("failed deletion changed snapshots", err)
	}
	flowSQL(t, `ALTER TABLE analytics_snapshots DISABLE TRIGGER analytics_prune_fail`)
	prune(1)
	if count() != 3 {
		t.Fatal("pruning did not recover")
	}
}
