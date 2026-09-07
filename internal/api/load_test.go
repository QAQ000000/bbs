package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"dzforum/internal/live"
)

type loadIdentity struct {
	id          int64
	token, csrf string
}

// Run explicitly with a disposable FORUM_TEST_DSN, -run '^$' -bench '^BenchmarkForumTraffic$' -benchtime=1x.
func BenchmarkForumTraffic(b *testing.B) {
	if smokePool == nil {
		b.Fatal("requires an isolated FORUM_TEST_DSN")
	}
	if b.N != 1 {
		b.Fatal("use -benchtime=1x")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	tx, err := smokePool.Begin(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var fid int64
	if err = tx.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'traffic') RETURNING id`).Scan(&fid); err != nil {
		b.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE threads DISABLE TRIGGER USER; ALTER TABLE posts DISABLE TRIGGER USER`); err != nil {
		b.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO threads(forum_id,author_id,title,post_count,floor_seq,last_post_at)
	 SELECT $1,1,'load topic '||g,5,5,current_date-g*interval '1 minute' FROM generate_series(1,10000) g`, fid); err != nil {
		b.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO posts(thread_id,author_id,floor,content_md,content_html,created_at)
	 SELECT t.id,1,g,repeat('load content ',40),'',t.last_post_at FROM threads t CROSS JOIN generate_series(1,5) g WHERE t.forum_id=$1`, fid); err != nil {
		b.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE threads t SET first_post_id=p.id FROM posts p WHERE p.thread_id=t.id AND p.floor=1 AND t.forum_id=$1`, fid); err != nil {
		b.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE threads ENABLE TRIGGER USER; ALTER TABLE posts ENABLE TRIGGER USER; ANALYZE threads; ANALYZE posts`); err != nil {
		b.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		b.Fatal(err)
	}
	if err = smokeSrv.st.RecomputeForumStats(ctx, fid); err != nil {
		b.Fatal(err)
	}
	rows, err := smokePool.Query(ctx, `INSERT INTO users(username,password_hash) SELECT 'load-user-'||g,'unusable' FROM generate_series(1,120) g RETURNING id`)
	if err != nil {
		b.Fatal(err)
	}
	people := []loadIdentity{}
	for rows.Next() {
		var u loadIdentity
		if err = rows.Scan(&u.id); err != nil {
			b.Fatal(err)
		}
		people = append(people, u)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		b.Fatal(err)
	}
	for i := range people {
		if _, err = smokePool.Exec(ctx, `UPDATE member_states SET level_id=4 WHERE user_id=$1`, people[i].id); err != nil {
			b.Fatal(err)
		}
		people[i].token, people[i].csrf, err = smokeSrv.st.CreateSession(ctx, people[i].id)
		if err != nil {
			b.Fatal(err)
		}
	}
	rows, err = smokePool.Query(ctx, `SELECT id FROM threads WHERE forum_id=$1 ORDER BY id LIMIT 20`, fid)
	if err != nil {
		b.Fatal(err)
	}
	tids := []int64{}
	for rows.Next() {
		var tid int64
		if err = rows.Scan(&tid); err != nil {
			b.Fatal(err)
		}
		tids = append(tids, tid)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		b.Fatal(err)
	}

	workers, stopWorkers := context.WithCancel(ctx)
	var workerWG sync.WaitGroup
	workerWG.Add(1)
	go func() { defer workerWG.Done(); smokeSrv.RunSubscriptions(workers) }()
	for _, task := range []struct {
		name  string
		run   func(context.Context, int) (int, error)
		limit int
	}{{"growth", smokeSrv.st.ProcessMemberEvents, 100}, {"titles", smokeSrv.st.ProcessTitleWork, 50}} {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-workers.Done():
					return
				case <-tick.C:
					job, done := context.WithTimeout(workers, 10*time.Second)
					_, err := task.run(job, task.limit)
					done()
					if workers.Err() == nil && err != nil {
						b.Errorf("background %s: %v", task.name, err)
					}
				}
			}
		}()
	}
	defer func() { stopWorkers(); workerWG.Wait() }()
	server := httptest.NewServer(smokeSrv.Handler())
	defer server.Close()
	transport := &http.Transport{MaxIdleConns: 200, MaxIdleConnsPerHost: 200}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	read := func(worker, iteration int) *http.Request {
		paths := []string{"/api/v1/home", fmt.Sprintf("/api/v1/threads?pagination=cursor&forumId=%d", fid), fmt.Sprintf("/api/v1/threads/%d/posts", tids[0]), fmt.Sprintf("/api/v1/threads/%d", tids[0]), "/api/v1/site"}
		r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+paths[iteration%len(paths)], nil)
		if worker%2 == 1 {
			r.AddCookie(&http.Cookie{Name: cookieSession, Value: people[worker%len(people)].token})
		}
		return r
	}
	loadHTTP(b, "mixed_reads_20", client, 20, 50, 200, read)
	loadHTTP(b, "mixed_reads_100", client, 100, 20, 200, read)
	for scenario := 0; scenario < 2; scenario++ {
		name := []string{"hot_thread_replies", "same_forum_replies"}[scenario]
		loadHTTP(b, name, client, 20, 8, 201, func(worker, iteration int) *http.Request {
			tid := tids[0]
			if scenario == 1 {
				tid = tids[worker]
			}
			u := people[worker+scenario*20]
			r, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v1/threads/%d/posts", server.URL, tid), strings.NewReader(`{"content":"load reply"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", u.csrf)
			r.AddCookie(&http.Cookie{Name: cookieSession, Value: u.token})
			return r
		})
	}
	var actual int
	if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM posts p JOIN threads t ON t.id=p.thread_id WHERE t.forum_id=$1 AND NOT p.pending AND NOT p.deleted`, fid).Scan(&actual); err != nil || actual != 50320 {
		b.Fatalf("public write count %d: %v", actual, err)
	}
	var exact bool
	if err = smokePool.QueryRow(ctx, `SELECT post_count=50320 AND thread_count=10000 FROM forums WHERE id=$1`, fid).Scan(&exact); err != nil || !exact {
		b.Fatal("forum counters drifted", err)
	}
	loadSSE(b, ctx, server.URL, client, people, tids[0], read)

	var subForum int64
	if err = smokePool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'fanout') RETURNING id`).Scan(&subForum); err != nil {
		b.Fatal(err)
	}
	if _, err = smokePool.Exec(ctx, `WITH u AS (INSERT INTO users(username,password_hash) SELECT 'load-follower-'||g,'unusable' FROM generate_series(1,500) g RETURNING id)
	 INSERT INTO forum_subscriptions(uid,forum_id) SELECT id,$1 FROM u`, subForum); err != nil {
		b.Fatal(err)
	}
	start := time.Now()
	_, post, err := smokeSrv.st.CreateThread(ctx, subForum, 1, "admin", "fanout", "body", "", false, "")
	if err != nil {
		b.Fatal(err)
	}
	for {
		if err = smokePool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE post_id=$1 AND type='subscription'`, post.ID).Scan(&actual); err != nil {
			b.Fatal(err)
		}
		if actual == 500 {
			break
		}
		if time.Since(start) > 20*time.Second {
			b.Fatal("fanout did not drain", actual)
		}
		time.Sleep(50 * time.Millisecond)
	}
	b.Logf("LOAD fanout recipients=500 elapsedMs=%d", time.Since(start).Milliseconds())
	drainStart := time.Now()
	for i := 0; i < 600; i++ {
		var growth, titles, subscriptions int
		if err = smokePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM member_events),(SELECT count(*) FROM title_events),
		 (SELECT count(*) FROM subscription_events e JOIN posts p ON p.id=e.post_id JOIN threads t ON t.id=p.thread_id
		 WHERE NOT e.completed AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted)`).Scan(&growth, &titles, &subscriptions); err != nil {
			b.Fatal(err)
		}
		if growth+titles+subscriptions == 0 {
			b.Logf("LOAD final queues: growth=0 titles=0 publicSubscriptions=0 drainMs=%d", time.Since(drainStart).Milliseconds())
			return
		}
		if i%50 == 0 {
			b.Logf("LOAD draining queues: growth=%d titles=%d publicSubscriptions=%d", growth, titles, subscriptions)
		}
		if i == 599 {
			b.Fatalf("queues did not drain: %d %d %d", growth, titles, subscriptions)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func loadHTTP(b *testing.B, name string, client *http.Client, workers, perWorker, want int, request func(int, int) *http.Request) {
	b.Helper()
	before := smokePool.Stat()
	start := time.Now()
	var mu sync.Mutex
	var wg sync.WaitGroup
	times := []float64{}
	errors := map[int]int{}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				begin := time.Now()
				resp, err := client.Do(request(w, i))
				status := 0
				if err == nil {
					status = resp.StatusCode
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				mu.Lock()
				times = append(times, float64(time.Since(begin).Microseconds())/1000)
				if err != nil || status != want {
					errors[status]++
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)
	slices.Sort(times)
	after := smokePool.Stat()
	b.Logf("LOAD %s concurrent=%d requests=%d rps=%.1f p95Ms=%.2f p99Ms=%.2f errors=%v dbAcquires=%d emptyAcquires=%d",
		name, workers, len(times), float64(len(times))/elapsed.Seconds(), times[(len(times)-1)*95/100], times[(len(times)-1)*99/100], errors,
		after.AcquireCount()-before.AcquireCount(), after.EmptyAcquireCount()-before.EmptyAcquireCount())
	if len(errors) > 0 {
		b.Errorf("%s returned unexpected statuses: %v", name, errors)
	}
}

func loadSSE(b *testing.B, ctx context.Context, base string, client *http.Client, people []loadIdentity, tid int64, read func(int, int) *http.Request) {
	b.Helper()
	streamCtx, stop := context.WithCancel(ctx)
	defer stop()
	streamClient := &http.Client{Transport: client.Transport}
	var wg sync.WaitGroup
	defer func() { stop(); wg.Wait() }()
	var mu sync.Mutex
	times := []float64{}
	counts := make([]int, 100)
	heartbeats := 0
	for i := 0; i < 100; i++ {
		r, _ := http.NewRequestWithContext(streamCtx, "GET", fmt.Sprintf("%s/api/v1/events?thread=%d", base, tid), nil)
		r.Header.Set("X-Real-IP", fmt.Sprintf("198.18.0.%d", i+1))
		r.AddCookie(&http.Cookie{Name: cookieSession, Value: people[i].token})
		resp, err := streamClient.Do(r)
		if err != nil {
			b.Fatal(err)
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			b.Fatal("SSE status", resp.StatusCode)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				mu.Lock()
				if line == ": ping" {
					heartbeats++
				}
				if strings.HasPrefix(line, "data: ") {
					var v struct {
						Type   string `json:"type"`
						SentAt int64  `json:"sentAt"`
					}
					if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &v) == nil && v.Type == "load.probe" {
						counts[i]++
						times = append(times, float64(time.Since(time.Unix(0, v.SentAt)).Microseconds())/1000)
					}
				}
				mu.Unlock()
			}
		}(i)
	}
	start := time.Now()
	readDone := make(chan struct{})
	go func() { defer close(readDone); loadHTTP(b, "reads_with_100_sse", client, 20, 20, 200, read) }()
	for i := 0; i < 20; i++ {
		payload, _ := json.Marshal(map[string]any{"type": "load.probe", "sentAt": time.Now().UnixNano()})
		smokeSrv.hub.Publish(live.Event{Topic: fmt.Sprintf("t:%d", tid), Payload: payload})
		time.Sleep(100 * time.Millisecond)
	}
	<-readDone
	if remaining := 26*time.Second - time.Since(start); remaining > 0 {
		time.Sleep(remaining)
	}
	stop()
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	for i, n := range counts {
		if n != 20 {
			b.Fatalf("SSE %d delivered %d/20 events", i, n)
		}
	}
	if heartbeats < 100 {
		b.Fatal("missing heartbeats", heartbeats)
	}
	slices.Sort(times)
	b.Logf("LOAD sse connections=100 deliveries=%d heartbeats=%d p95Ms=%.2f p99Ms=%.2f", len(times), heartbeats, times[(len(times)-1)*95/100], times[(len(times)-1)*99/100])
}
