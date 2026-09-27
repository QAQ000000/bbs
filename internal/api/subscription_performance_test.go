package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"dzforum/internal/live"
	"dzforum/internal/store"
)

func TestSubscriptionBurstLimitsAndStops(t *testing.T) {
	ctx := context.Background()
	calls := 0
	n, err := processSubscriptionBurst(ctx, func(context.Context) (int64, error) { calls++; return 1, nil }, func() bool { return false })
	if err != nil || n.batches <= 20 || n.batches > 100 || calls != n.batches {
		t.Fatal(n, calls, err)
	}
	calls = 0
	n, err = processSubscriptionBurst(ctx, func(context.Context) (int64, error) { calls++; return 0, nil }, func() bool { return false })
	if err != nil || n.batches != 0 || calls != 1 {
		t.Fatal(n, calls, err)
	}
	failure := errors.New("failed delivery")
	n, err = processSubscriptionBurst(ctx, func(context.Context) (int64, error) { return 1, failure }, func() bool { return false })
	if n.batches != 0 || !errors.Is(err, failure) {
		t.Fatal(n, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	n, err = processSubscriptionBurst(canceled, func(context.Context) (int64, error) { t.Fatal("called with canceled context"); return 0, nil }, func() bool { return false })
	if n.batches != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
	n, err = processSubscriptionBurst(ctx, func(context.Context) (int64, error) { time.Sleep(260 * time.Millisecond); return 1, nil }, func() bool { return false })
	if n.batches != 1 || err != nil {
		t.Fatal("soft budget split or repeated a completed transaction", n, err)
	}
}

// Compare post-commit SSE count work on the same 20 recipients and 100000
// unread records. This does not benchmark durable insertion or full Worker RPS.
func BenchmarkSubscriptionCounts(b *testing.B) {
	if smokePool == nil {
		b.Fatal("requires isolated FORUM_TEST_DSN")
	}
	ctx := context.Background()
	rows, err := smokePool.Query(ctx, `INSERT INTO users(username,password_hash) SELECT 'counts-bench-'||g,'unusable' FROM generate_series(1,20) g RETURNING id`)
	if err != nil {
		b.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			b.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}
	if _, err := smokePool.Exec(ctx, `INSERT INTO notifications(uid,from_uid,from_name,type,thread_id,post_id,excerpt) SELECT uid,1,'admin','subscription',1,1,'benchmark' FROM unnest($1::bigint[]) uid CROSS JOIN generate_series(1,5000)`, ids); err != nil {
		b.Fatal(err)
	}
	if _, err := smokePool.Exec(ctx, `ANALYZE notifications`); err != nil {
		b.Fatal(err)
	}
	for _, online := range []int{0, 1, 20} {
		for _, variant := range []string{"before", "after"} {
			b.Run(fmt.Sprintf("online_%d/%s", online, variant), func(b *testing.B) {
				s := &Server{st: smokeSrv.st, hub: live.NewHub()}
				var subs []*live.Subscriber
				for _, id := range ids[:online] {
					sub, cancel := s.hub.Subscribe(2, "u:"+idString(id))
					subs = append(subs, sub)
					defer cancel()
				}
				run := func() {
					if variant == "before" {
						counts, err := s.st.NotificationCounts(ctx, ids)
						if err != nil {
							b.Fatal(err)
						}
						for uid, count := range counts {
							s.publish("u:"+idString(uid), eventBody{Type: "notify", NotifyCount: int(count)})
						}
					} else if err := s.publishSubscriptionCounts(ctx, ids); err != nil {
						b.Fatal(err)
					}
					for _, sub := range subs {
						select {
						case <-sub.C():
						default:
							b.Fatal("live event missing")
						}
					}
				}
				run()
				start := smokePool.Stat().AcquireCount()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					run()
				}
				b.StopTimer()
				b.ReportMetric(float64(smokePool.Stat().AcquireCount()-start)/float64(b.N), "dbacquires/op")
			})
		}
	}
}

func TestSubscriptionCountsOfflineSkipsDatabase(t *testing.T) {
	// A nil store makes accidental offline database work fail immediately.
	s := &Server{hub: live.NewHub()}
	_, cancel := s.hub.Subscribe(1, "t:1")
	defer cancel()
	if err := s.publishSubscriptionCounts(context.Background(), []int64{1, 2, 1}); err != nil {
		t.Fatal(err)
	}
	_, cancelUser := s.hub.Subscribe(1, "u:1")
	cancelUser()
	if err := s.publishSubscriptionCounts(context.Background(), []int64{1}); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionCountsOnlineFreshAndDeduplicated(t *testing.T) {
	u, _, _ := memberTestUser(t)
	ctx := context.Background()
	s := &Server{st: smokeSrv.st, hub: live.NewHub()}
	sub, cancel := s.hub.Subscribe(4, "u:"+idString(u.ID))
	defer cancel()
	if _, err := smokePool.Exec(ctx, `INSERT INTO notifications(uid,from_uid,from_name,type,scope,thread_id,post_id,excerpt) VALUES($1,1,'admin','test','account',0,0,'test')`, u.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{1, 0} {
		if err := s.publishSubscriptionCounts(ctx, []int64{u.ID, u.ID}); err != nil {
			t.Fatal(err)
		}
		select {
		case raw := <-sub.C():
			var ev eventBody
			if err := json.Unmarshal(raw, &ev); err != nil || ev.Type != "notify" || ev.NotifyCount != want {
				t.Fatalf("event=%s err=%v", raw, err)
			}
		default:
			t.Fatal("connected user missed count update")
		}
		select {
		case <-sub.C():
			t.Fatal("duplicate user produced duplicate update")
		default:
		}
		if _, err := smokePool.Exec(ctx, `UPDATE notifications SET read=true WHERE uid=$1`, u.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSubscriptionOfflineDeliveryPersists(t *testing.T) {
	u, _, _ := memberTestUser(t)
	ctx := context.Background()
	var fid int64
	if err := smokePool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'offline-delivery') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if _, err := smokeSrv.st.SaveSubscription(ctx, u.ID, store.Subscription{Kind: "forum", TargetID: fid}, true); err != nil {
		t.Fatal(err)
	}
	_, p, err := smokeSrv.st.CreateThread(ctx, fid, 1, "admin", "offline notification", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{st: smokeSrv.st, hub: live.NewHub(), mailer: smokeSrv.mailer}
	for i := 0; i < 2000; i++ {
		pid, err := s.ProcessSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if pid == 0 {
			break
		}
	}
	var count int
	if err := smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE uid=$1 AND post_id=$2 AND type='subscription'`, u.ID, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable notifications=%d err=%v", count, err)
	}
	if err := smokePool.QueryRow(ctx, `SELECT count(*) FROM subscription_events WHERE post_id=$1 AND NOT completed`, p.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unfinished events=%d err=%v", count, err)
	}
	counts, err := smokeSrv.st.NotificationCounts(ctx, []int64{u.ID})
	if err != nil || counts[u.ID] != 1 {
		t.Fatal("offline API count", counts, err)
	}
}

func TestSubscriptionBurstCoalescesCommittedNotifications(t *testing.T) {
	u, _, _ := memberTestUser(t)
	ctx := context.Background()
	s := &Server{st: smokeSrv.st, hub: live.NewHub(), mailer: smokeSrv.mailer}
	// Complete earlier public test work before measuring this user's burst.
	for i := 0; i < 2000; i++ {
		pid, err := s.ProcessSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if pid == 0 {
			break
		}
	}
	var fid int64
	if err := smokePool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'burst-counts') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.SaveSubscription(ctx, u.ID, store.Subscription{Kind: "forum", TargetID: fid}, true); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, _, err := s.st.CreateThread(ctx, fid, 1, "admin", "coalesced", "body", "", false, ""); err != nil {
			t.Fatal(err)
		}
	}
	sub, cancel := s.hub.Subscribe(4, "u:"+idString(u.ID))
	defer cancel()
	n, err := s.processSubscriptionWork(ctx)
	if err != nil || n.batches != 3 {
		t.Fatal("burst", n, err)
	}
	select {
	case raw := <-sub.C():
		var ev eventBody
		if err := json.Unmarshal(raw, &ev); err != nil || ev.NotifyCount != 3 {
			t.Fatalf("event=%s err=%v", raw, err)
		}
	default:
		t.Fatal("committed counts not published")
	}
	select {
	case <-sub.C():
		t.Fatal("burst published more than once per user")
	default:
	}
	counts, err := s.st.NotificationCounts(ctx, []int64{u.ID})
	if err != nil || counts[u.ID] != 3 {
		t.Fatal("durable notifications were coalesced", counts, err)
	}
}

func TestSubscriptionBurstPublishesEarlierCommitsOnFailure(t *testing.T) {
	u, _, _ := memberTestUser(t)
	ctx := context.Background()
	s := &Server{st: smokeSrv.st, hub: live.NewHub(), mailer: smokeSrv.mailer}
	for i := 0; i < 2000; i++ {
		pid, err := s.ProcessSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if pid == 0 {
			break
		}
	}
	var fid int64
	if err := smokePool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'burst-failure') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.SaveSubscription(ctx, u.ID, store.Subscription{Kind: "forum", TargetID: fid}, true); err != nil {
		t.Fatal(err)
	}
	var badID int64
	for range 2 {
		_, p, err := s.st.CreateThread(ctx, fid, 1, "admin", "partial burst", "body", "", false, "")
		if err != nil {
			t.Fatal(err)
		}
		badID = p.ID
	}
	if _, err := smokePool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION fail_live_burst() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'burst failure'; END $$;
        CREATE TRIGGER fail_live_burst BEFORE INSERT ON subscription_deliveries FOR EACH ROW WHEN (NEW.post_id=%d) EXECUTE FUNCTION fail_live_burst()`, badID)); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		if _, err := smokePool.Exec(ctx, `DROP TRIGGER IF EXISTS fail_live_burst ON subscription_deliveries; DROP FUNCTION IF EXISTS fail_live_burst()`); err != nil {
			t.Error(err)
		}
	}
	defer cleanup()
	sub, cancel := s.hub.Subscribe(4, "u:"+idString(u.ID))
	defer cancel()
	n, err := s.processSubscriptionWork(ctx)
	if err == nil || n.batches != 1 {
		t.Fatal("failed burst", n, err)
	}
	check := func(want int) {
		t.Helper()
		select {
		case raw := <-sub.C():
			var ev eventBody
			if err := json.Unmarshal(raw, &ev); err != nil || ev.NotifyCount != want {
				t.Fatalf("event=%s want=%d err=%v", raw, want, err)
			}
		default:
			t.Fatal("missing committed count")
		}
	}
	check(1)
	var completed bool
	if err := smokePool.QueryRow(ctx, `SELECT completed FROM subscription_events WHERE post_id=$1`, badID).Scan(&completed); err != nil || completed {
		t.Fatal("failed event lost", completed, err)
	}
	cleanup()
	n, err = s.processSubscriptionWork(ctx)
	if err != nil || n.batches != 1 {
		t.Fatal("retry", n, err)
	}
	check(2)
	counts, err := s.st.NotificationCounts(ctx, []int64{u.ID})
	if err != nil || counts[u.ID] != 2 {
		t.Fatal("retry duplicated or dropped notifications", counts, err)
	}
}
