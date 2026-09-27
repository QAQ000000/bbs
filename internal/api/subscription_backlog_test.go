package api

import (
	"context"
	"testing"
	"time"
)

// Compare scheduling only: same durable delivery path, one worker, no online
// clients/SMTP/foreground traffic. This is not a replacement for HTTP soak.
func BenchmarkSubscriptionBacklog(b *testing.B) {
	if smokePool == nil {
		b.Fatal("requires isolated FORUM_TEST_DSN")
	}
	if smokeSrv.mailer.Enabled() {
		b.Fatal("requires SMTP disabled")
	}
	ctx := context.Background()
	const posts = 2000
	const recipients = 20
	rows, err := smokePool.Query(ctx, `INSERT INTO users(username,password_hash) SELECT 'backlog-'||g,'unusable' FROM generate_series(1,$1::int) g RETURNING id`, recipients)
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
	var fid int64
	if err := smokePool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'backlog benchmark') RETURNING id`).Scan(&fid); err != nil {
		b.Fatal(err)
	}
	th, _, err := smokeSrv.st.CreateThread(ctx, fid, 1, "admin", "backlog benchmark", "body", "", false, "")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := smokePool.Exec(ctx, `INSERT INTO thread_subscriptions(uid,thread_id) SELECT unnest($1::bigint[]),$2`, ids, th.ID); err != nil {
		b.Fatal(err)
	}
	b.Logf("BACKLOG posts=%d recipients=%d poolMax=%d poolMin=%d smtp=false online=0", posts, recipients, smokePool.Config().MaxConns, smokePool.Config().MinConns)
	for _, variant := range []string{"fixed_tick", "bounded_catchup"} {
		b.Run(variant, func(b *testing.B) {
			b.StopTimer()
			var total time.Duration
			for range b.N {
				// Dedicated benchmark database; repeat exactly the same payload size and
				// clear prior derived deliveries before each timed variant/iteration.
				if _, err := smokePool.Exec(ctx, `TRUNCATE subscription_events,subscription_deliveries,notifications`); err != nil {
					b.Fatal(err)
				}
				if _, err := smokePool.Exec(ctx, `DELETE FROM posts WHERE thread_id=$1 AND floor>1`, th.ID); err != nil {
					b.Fatal(err)
				}
				if _, err := smokePool.Exec(ctx, `INSERT INTO posts(thread_id,author_id,floor,content_md,content_html) SELECT $1,1,g+1,'backlog benchmark reply','' FROM generate_series(1,$2::int) g`, th.ID, posts); err != nil {
					b.Fatal(err)
				}
				if _, err := smokePool.Exec(ctx, `ANALYZE subscription_events; ANALYZE posts; ANALYZE notifications`); err != nil {
					b.Fatal(err)
				}
				job, cancel := context.WithTimeout(ctx, 3*time.Minute)
				done := make(chan struct{})
				workerErrors := make(chan error, 1)
				start := time.Now()
				b.StartTimer()
				go func() {
					defer close(done)
					if variant == "bounded_catchup" {
						smokeSrv.RunSubscriptions(job)
						return
					}
					// Reproduce the previous one-second ticker. The durable work and per-round
					// 100-batch/250ms bounds are shared with the new variant.
					tick := time.NewTicker(time.Second)
					defer tick.Stop()
					for {
						select {
						case <-job.Done():
							return
						case <-tick.C:
						}
						round, stop := context.WithTimeout(job, 10*time.Second)
						_, err := smokeSrv.processSubscriptionWork(round)
						stop()
						if err != nil && job.Err() == nil {
							workerErrors <- err
							return
						}
					}
				}()
				var runErr error
				for {
					var pending int
					if runErr = smokePool.QueryRow(job, `SELECT count(*) FROM subscription_events WHERE NOT completed`).Scan(&pending); runErr != nil {
						break
					}
					if pending == 0 {
						break
					}
					select {
					case runErr = <-workerErrors:
					case <-job.Done():
						runErr = job.Err()
					case <-time.After(25 * time.Millisecond):
					}
					if runErr != nil {
						break
					}
				}
				elapsed := time.Since(start)
				cancel()
				<-done
				b.StopTimer()
				if runErr != nil {
					b.Fatal(runErr)
				}
				total += elapsed
				var notifications, unique, deliveries, completed int
				err := smokePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE type='subscription'),(SELECT count(DISTINCT (uid,post_id)) FROM notifications WHERE type='subscription'),(SELECT count(*) FROM subscription_deliveries),(SELECT count(*) FROM subscription_events WHERE completed)`).Scan(&notifications, &unique, &deliveries, &completed)
				if err != nil || notifications != posts*recipients || unique != notifications || deliveries != notifications || completed != posts {
					b.Fatalf("notifications=%d unique=%d deliveries=%d completed=%d err=%v", notifications, unique, deliveries, completed, err)
				}
				b.Logf("BACKLOG variant=%s seconds=%.3f events=%d notifications=%d consistency=true", variant, elapsed.Seconds(), completed, notifications)
			}
			b.ReportMetric(total.Seconds()/float64(b.N), "drain_s/op")
			b.ReportMetric(float64(posts*b.N)/total.Seconds(), "events/s")
			b.ReportMetric(float64(posts*recipients), "notifications/op")
		})
	}
}
