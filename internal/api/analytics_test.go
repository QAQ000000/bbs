package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestAnalyticsWorkerRetryAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		start := time.Now()
		var mu sync.Mutex
		var attempts, retries []time.Duration
		siteCalls, retentionCalls := 0, 0
		failure := errors.New("database unavailable")
		go runAnalyticsWorker(ctx, []analyticsJob{
			{name: "points", refresh: func(context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				attempts = append(attempts, time.Since(start))
				if len(attempts) < 8 || len(attempts) == 9 {
					return failure
				}
				return nil
			}},
			{name: "site", refresh: func(context.Context) error { mu.Lock(); siteCalls++; mu.Unlock(); return nil }},
			{name: "retention", interval: analyticsRetentionInterval, refresh: func(context.Context) error { mu.Lock(); retentionCalls++; mu.Unlock(); return nil }},
		}, func(name string, err error, retry time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			if name != "points" || !errors.Is(err, failure) {
				t.Error(name, err)
			}
			retries = append(retries, retry)
		})
		time.Sleep(615 * time.Second)
		synctest.Wait()
		want := []time.Duration{0, 5 * time.Second, 15 * time.Second, 35 * time.Second, 75 * time.Second, 155 * time.Second, 315 * time.Second, 615 * time.Second}
		mu.Lock()
		if !reflect.DeepEqual(attempts, want) || siteCalls != 1 || retentionCalls != 11 {
			t.Errorf("attempts=%v siteCalls=%d retentionCalls=%d", attempts, siteCalls, retentionCalls)
		}
		mu.Unlock()
		// A successful refresh returns to hourly work; the next failure resets backoff.
		time.Sleep(time.Hour)
		synctest.Wait()
		mu.Lock()
		if len(attempts) != 9 || siteCalls != 2 || retentionCalls != 71 || retries[len(retries)-1] != 5*time.Second {
			t.Error(attempts, retries, siteCalls, retentionCalls)
		}
		mu.Unlock()
		cancel()
		synctest.Wait() // Must interrupt the pending retry without waiting five seconds.
		if len(attempts) != 9 || time.Since(start) != 4215*time.Second {
			t.Fatal("retry continued after shutdown", attempts)
		}
	})
}

func TestAnalyticsWorkerIndependentTimeoutAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		siteStarted, siteStopped, reports := false, false, 0
		go runAnalyticsWorker(ctx, []analyticsJob{
			{name: "points", refresh: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			}},
			{name: "site", refresh: func(ctx context.Context) error {
				siteStarted = true
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) != analyticsJobTimeout {
					t.Error("previous job consumed site deadline")
				}
				<-ctx.Done()
				siteStopped = true
				return ctx.Err()
			}},
		}, func(string, error, time.Duration) { reports++ })
		time.Sleep(analyticsJobTimeout)
		synctest.Wait()
		if !siteStarted || siteStopped || reports != 1 {
			t.Fatal(siteStarted, siteStopped, reports)
		}
		cancel()
		synctest.Wait()
		if !siteStopped || reports != 1 {
			t.Fatal("shutdown not propagated or logged as failure", siteStopped, reports)
		}
	})
}

func TestAnalyticsSnapshotFreshnessAndFailedRefresh(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	period := time.Now().UTC().Truncate(time.Hour)
	if err := smokeSrv.st.RefreshPointsLeaderboard(ctx, period, 100); err != nil {
		t.Fatal(err)
	}
	if err := smokeSrv.st.RefreshSiteReport(ctx, period.Truncate(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	doc := loadAPIContract(t)
	read := func(name string, stale bool) contractObject {
		t.Helper()
		body := assertContractResponse(t, doc, "GET", "/api/v1/admin/analytics/{name}", smokeGet(t, "/api/v1/admin/analytics/"+name, adminCookie), 200)
		v := body["data"].(contractObject)
		if v["stale"] != stale || v["refreshIntervalSeconds"] != float64(3600) || v["staleAfterSeconds"] != float64(3900) || v["ageSeconds"].(float64) < 0 {
			t.Fatal(v)
		}
		return v
	}
	read("points", false)
	read("site", false)
	for _, tc := range []struct {
		cookie *http.Cookie
		status int
	}{{nil, 401}, {userCookie, 403}, {modCookie, 403}} {
		checkJSON(t, smokeGet(t, "/api/v1/admin/analytics/points", tc.cookie), tc.status)
	}
	checkJSON(t, smokeGet(t, "/api/v1/admin/analytics/not-generated", adminCookie), 404)
	flowSQL(t, `UPDATE analytics_snapshots SET generated_at=now()-interval '2 hours' WHERE name='points'`)
	old, err := smokeSrv.st.LatestAnalyticsSnapshot(ctx, "points")
	if err != nil {
		t.Fatal(err)
	}
	read("points", true)
	flowSQL(t, `CREATE FUNCTION analytics_refresh_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name='points' THEN RAISE EXCEPTION 'injected snapshot failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER analytics_refresh_fail BEFORE INSERT OR UPDATE ON analytics_snapshots FOR EACH ROW EXECUTE FUNCTION analytics_refresh_fail()`)
	t.Cleanup(func() {
		flowSQL(t, `DROP TRIGGER analytics_refresh_fail ON analytics_snapshots; DROP FUNCTION analytics_refresh_fail()`)
	})
	if err := smokeSrv.st.RefreshPointsLeaderboard(ctx, period, 100); err == nil {
		t.Fatal("injected refresh did not fail")
	}
	after, err := smokeSrv.st.LatestAnalyticsSnapshot(ctx, "points")
	if err != nil || !reflect.DeepEqual(old, after) {
		t.Fatal("failed refresh replaced last good snapshot", old, after, err)
	}
	read("points", true)
	if err := smokeSrv.st.RefreshSiteReport(ctx, period.Truncate(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	read("site", false)
	flowSQL(t, `ALTER TABLE analytics_snapshots DISABLE TRIGGER analytics_refresh_fail`)
	if err := smokeSrv.st.RefreshPointsLeaderboard(ctx, period, 100); err != nil {
		t.Fatal(err)
	}
	read("points", false)
}
