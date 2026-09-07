package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConcurrencySubscriptionSingleConnectionAndRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	author, _ := setupUsers(t)
	fid := setupForum(t)
	rows, err := testPool.Query(ctx, `INSERT INTO users(username,password_hash,email)
	 SELECT $1||g,'hash',$1||g||'@example.test' FROM generate_series(1,50) g RETURNING id`, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for rows.Next() {
		var uid int64
		if err = rows.Scan(&uid); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, uid)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `INSERT INTO forum_subscriptions(uid,forum_id) SELECT unnest($1::bigint[]),$2`, ids, fid); err != nil {
		t.Fatal(err)
	}
	th, p, err := testStore.CreateThread(ctx, fid, author, "author", "bulk subscription", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testPool.Config()
	cfg.MaxConns, cfg.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := New(pool)
	// Failure after delivery claims must roll back claims, notifications, mail and cursor.
	if _, err = testPool.Exec(ctx, `CREATE FUNCTION fail_bulk_notify() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'bulk failure'; END $$;
	 CREATE TRIGGER fail_bulk_notify BEFORE INSERT ON notifications FOR EACH ROW EXECUTE FUNCTION fail_bulk_notify()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_bulk_notify ON notifications; DROP FUNCTION IF EXISTS fail_bulk_notify()`)
	})
	if _, err = s.ProcessSubscriptionBatch(ctx, 50, p.ID, true); err == nil {
		t.Fatal("notification failure ignored")
	}
	var clean bool
	if err = testPool.QueryRow(ctx, `SELECT cursor_uid=0 AND NOT completed
	 AND NOT EXISTS(SELECT 1 FROM subscription_deliveries WHERE post_id=$1)
	 AND NOT EXISTS(SELECT 1 FROM email_jobs WHERE post_id=$1) FROM subscription_events WHERE post_id=$1`, p.ID).Scan(&clean); err != nil || !clean {
		t.Fatal("partial commit", clean, err)
	}
	if _, err = testPool.Exec(ctx, `DROP TRIGGER fail_bulk_notify ON notifications; DROP FUNCTION fail_bulk_notify()`); err != nil {
		t.Fatal(err)
	}
	before := pool.Stat().AcquireCount()
	batch, err := s.ProcessSubscriptionBatch(ctx, 50, p.ID, true)
	if err != nil || len(batch.Deliveries) != 50 {
		t.Fatal(len(batch.Deliveries), err)
	}
	if acquired := pool.Stat().AcquireCount() - before; acquired != 1 {
		t.Fatal("nested connection acquisition", acquired)
	}
	var n, mail int
	if err = testPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE post_id=$1),(SELECT count(*) FROM email_jobs WHERE post_id=$1)`, p.ID).Scan(&n, &mail); err != nil || n != 50 || mail != 50 {
		t.Fatal(n, mail, err)
	}
	if err = testPool.QueryRow(ctx, `SELECT completed FROM subscription_events WHERE post_id=$1`, p.ID).Scan(&clean); err != nil || !clean {
		t.Fatal("full final batch was not completed", err)
	}
	counts, err := s.NotificationCounts(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range ids {
		if counts[uid] != 1 {
			t.Fatal("incorrect unread count", uid, counts[uid])
		}
	}
	if err = s.SetPostPendingModeration(ctx, p.ID, "hide"); err != nil {
		t.Fatal(err)
	}
	counts, err = s.NotificationCounts(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range counts {
		if n != 0 {
			t.Fatal("hidden notification counted", n)
		}
	}
	if err = s.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProcessSubscriptionBatch(ctx, 50, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = testPool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE post_id=$1 AND type='subscription'`, p.ID).Scan(&n); err != nil || n != 50 {
		t.Fatal("duplicate deliveries", n, err)
	}
}

func TestConcurrencyDeliveryAndHotReplies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a, b := setupUsers(t)
	fid := setupForum(t)
	if _, err := testStore.SaveSubscription(ctx, b, Subscription{Kind: "forum", TargetID: fid}, true); err != nil {
		t.Fatal(err)
	}
	th, p, err := testStore.CreateThread(ctx, fid, a, "author", "hot", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_, err := testStore.ProcessSubscriptionBatch(ctx, 50, p.ID, false)
				errs <- err
			} else {
				_, _, err := testStore.CreateReply(ctx, th.ID, a, "author", fmt.Sprint(i), "", false, "")
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = testStore.ProcessSubscriptionBatch(ctx, 50, p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err = testPool.QueryRow(ctx, `SELECT f.post_count=7 AND f.thread_count=1 AND t.post_count=7
	 AND (SELECT count(DISTINCT floor) FROM posts WHERE thread_id=t.id)=7
	 AND (SELECT count(*) FROM notifications WHERE uid=$3 AND post_id=$4)=1
	 FROM forums f JOIN threads t ON t.forum_id=f.id WHERE f.id=$1 AND t.id=$2`, fid, th.ID, b, p.ID).Scan(&valid); err != nil || !valid {
		t.Fatal(valid, err)
	}
}
