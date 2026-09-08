package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dzforum/internal/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type soakProof struct {
	Stage int
	Sent  time.Time
}

type soakTraffic struct {
	sync.Mutex
	Times  []float64
	Errors map[string]int
	Missed int
}

func soakPercentile(v []float64, p int) float64 {
	if len(v) == 0 {
		return 0
	}
	c := slices.Clone(v)
	slices.Sort(c)
	return c[(len(c)-1)*p/100]
}

func (s *soakTraffic) result() map[string]any {
	s.Lock()
	defer s.Unlock()
	return map[string]any{"requests": len(s.Times), "p95Ms": soakPercentile(s.Times, 95), "p99Ms": soakPercentile(s.Times, 99), "errors": s.Errors, "missed": s.Missed}
}

// Arrival times are fixed independently of response speed. Saturated clients
// count missed arrivals instead of silently reducing the offered rate.
func soakTrafficRun(ctx context.Context, duration time.Duration, rps, workers int, makeRequest func(int) error, out *soakTraffic) {
	type arrival struct {
		seq int
		at  time.Time
	}
	jobs := make(chan arrival, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				err := makeRequest(job.seq)
				out.Lock()
				out.Times = append(out.Times, float64(time.Since(job.at).Microseconds())/1000)
				if err != nil {
					out.Errors[err.Error()]++
				}
				out.Unlock()
			}
		}()
	}
	start := time.Now()
	total := int(duration.Seconds() * float64(rps))
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	next := 0
	for next < total && ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case now := <-tick.C:
			due := min(total, int(now.Sub(start).Seconds()*float64(rps)))
			for next < due {
				select {
				case jobs <- arrival{next, start.Add(time.Duration(int64(next+1) * int64(time.Second) / int64(rps)))}:
				default:
					out.Lock()
					out.Missed++
					out.Unlock()
				}
				next++
			}
		}
	}
	close(jobs)
	wg.Wait()
}

func TestSoakArrivalAccounting(t *testing.T) {
	for _, tc := range []struct {
		name      string
		delay     time.Duration
		saturated bool
	}{
		{"available", 0, false}, {"saturated", 20 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := &soakTraffic{Errors: map[string]int{}}
			soakTrafficRun(context.Background(), 200*time.Millisecond, 200, 1, func(int) error { time.Sleep(tc.delay); return nil }, stats)
			if len(stats.Times)+stats.Missed != 40 {
				t.Fatalf("lost arrival accounting: completed=%d missed=%d", len(stats.Times), stats.Missed)
			}
			if tc.saturated && stats.Missed == 0 {
				t.Fatal("saturated client silently reduced offered traffic")
			}
			if len(stats.Errors) != 0 {
				t.Fatal(stats.Errors)
			}
		})
	}
}

func loadSustained(b *testing.B, ctx context.Context, client *http.Client, base string, fid int64, tids []int64, people []loadIdentity, duration time.Duration) {
	b.Helper()
	path := filepath.Join(os.Getenv("FORUM_TEST_RESULTS_DIR"), "soak.jsonl")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	var logMu sync.Mutex
	enc := json.NewEncoder(f)
	emit := func(v any) {
		logMu.Lock()
		defer logMu.Unlock()
		if err := enc.Encode(v); err != nil {
			b.Error(err)
		}
	}
	probeConfig := smokePool.Config().Copy()
	probeConfig.MinConns, probeConfig.MaxConns = 0, 2
	probe, err := pgxpool.NewWithConfig(ctx, probeConfig)
	if err != nil {
		b.Fatal(err)
	}
	defer probe.Close()
	// Seed full historical vectors only in this disposable fixture, before load.
	if _, err = probe.Exec(ctx, `UPDATE posts p SET search_data=to_tsvector('simple',p.content_md) FROM threads t WHERE t.id=p.thread_id AND t.forum_id=$1`, fid); err != nil {
		b.Fatal(err)
	}
	if _, err = probe.Exec(ctx, `WITH u AS (INSERT INTO users(username,password_hash) SELECT 'soak-subscriber-'||g,'unusable' FROM generate_series(1,20) g RETURNING id) INSERT INTO thread_subscriptions(uid,thread_id) SELECT u.id,t.id FROM u CROSS JOIN threads t WHERE t.id=ANY($1)`, tids); err != nil {
		b.Fatal(err)
	}
	warmup := time.Now()
	for {
		var left int64
		if err = probe.QueryRow(ctx, `SELECT (SELECT count(*) FROM member_events)+(SELECT count(*) FROM title_events)+(SELECT count(*) FROM search_index_events)+(SELECT count(*) FROM forum_stat_events)`).Scan(&left); err != nil {
			b.Fatal(err)
		}
		if left == 0 {
			break
		}
		if time.Since(warmup) > time.Minute {
			b.Fatal("initial fixture queues did not drain")
		}
		time.Sleep(200 * time.Millisecond)
	}
	var pgSettings json.RawMessage
	if err = probe.QueryRow(ctx, `SELECT json_object_agg(name,setting) FROM pg_settings WHERE name IN ('server_version','max_connections','shared_buffers','work_mem','effective_cache_size','checkpoint_timeout','max_wal_size','track_io_timing','track_wal_io_timing','fsync','synchronous_commit')`).Scan(&pgSettings); err != nil {
		b.Fatal(err)
	}
	emit(map[string]any{"kind": "config", "at": time.Now(), "stageSeconds": duration.Seconds(), "readRates": []int{200, 400, 600}, "writeRates": []int{5, 15, 30}, "broadSearchPercent": 2, "cpus": runtime.NumCPU(), "pool": db.Snapshot(smokePool), "observerConnections": 2, "identities": len(people), "subscribers": 20, "pgSettings": pgSettings})
	var pendingMu sync.Mutex
	pending := map[int64]soakProof{}
	var delays []float64
	delaysByStage := map[int][]float64{}
	lastDelaySample := 0
	var accepted int64
	var monitorErrors int
	var currentStage int
	var traffic [3][2]*soakTraffic
	for i := range traffic {
		for j := range traffic[i] {
			traffic[i][j] = &soakTraffic{Errors: map[string]int{}}
		}
	}
	resource := newSoakResources(b)
	start := time.Now()
	sample := func() {
		job, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		var data json.RawMessage
		err := probe.QueryRow(job, `SELECT json_build_object(
'search', (SELECT count(*) FROM search_index_events),
'searchOldestSeconds', (SELECT coalesce(extract(epoch FROM clock_timestamp()-min(created_at)),0) FROM search_index_events),
'searchRetries', (SELECT count(*) FROM search_index_events WHERE attempts>0),
'forum', (SELECT count(*) FROM forum_stat_events),
'forumOldestSeconds', (SELECT coalesce(extract(epoch FROM clock_timestamp()-min(created_at)),0) FROM forum_stat_events),
'growth', (SELECT count(*) FROM member_events), 'titles', (SELECT count(*) FROM title_events),
'subscriptions', (SELECT count(*) FROM subscription_events e JOIN posts p ON p.id=e.post_id JOIN threads t ON t.id=p.thread_id WHERE NOT e.completed AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted),
'subscriptionOldestSeconds', (SELECT coalesce(extract(epoch FROM clock_timestamp()-min(e.created_at)),0) FROM subscription_events e JOIN posts p ON p.id=e.post_id JOIN threads t ON t.id=p.thread_id WHERE NOT e.completed AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted),
'email', (SELECT count(*) FROM email_jobs WHERE status IN ('pending','sending')),
'lockWaits', (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),
'maxLockWaitSeconds', (SELECT coalesce(max(extract(epoch FROM clock_timestamp()-query_start)),0) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),
'blockers', (SELECT coalesce(json_agg(json_build_object('pid',pid,'blockedBy',pg_blocking_pids(pid))),'[]') FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),
'database', (SELECT row_to_json(d) FROM (SELECT xact_commit,xact_rollback,blks_read,blks_hit,blk_read_time,blk_write_time,temp_bytes,deadlocks FROM pg_stat_database WHERE datname=current_database()) d),
'io', (SELECT coalesce(json_agg(i),'[]') FROM (SELECT backend_type,object,context,reads,writes,read_time,write_time,fsyncs,fsync_time FROM pg_stat_io) i),
'wal', (SELECT row_to_json(w) FROM (SELECT wal_records,wal_bytes,wal_buffers_full FROM pg_stat_wal) w))`).Scan(&data)
		pendingMu.Lock()
		stage := currentStage
		indexPending, indexed := len(pending), len(delays)
		recentDelays := slices.Clone(delays[lastDelaySample:])
		lastDelaySample = len(delays)
		oldest := float64(0)
		for _, p := range pending {
			oldest = max(oldest, time.Since(p.Sent).Seconds())
		}
		pendingMu.Unlock()
		resources := resource.sample()
		if resources["error"] != nil {
			monitorErrors++
		}
		v := map[string]any{"kind": "sample", "at": time.Now(), "elapsedSeconds": time.Since(start).Seconds(), "stage": stage, "pool": db.Snapshot(smokePool), "resources": resources, "indexPending": indexPending, "indexed": indexed, "oldestUnindexedSeconds": oldest, "recentSearchP95Ms": soakPercentile(recentDelays, 95)}
		if stage >= 1 && stage <= 3 {
			for i, name := range []string{"reads", "writes"} {
				s := traffic[stage-1][i]
				s.Lock()
				failures := 0
				for _, n := range s.Errors {
					failures += n
				}
				v[name] = map[string]int{"completed": len(s.Times), "errors": failures, "missed": s.Missed}
				s.Unlock()
			}
		}
		if err != nil {
			v["error"] = err.Error()
			monitorErrors++
		} else {
			v["db"] = data
		}
		emit(v)
	}
	checkIndexes := func() {
		pendingMu.Lock()
		ids := make([]int64, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}
		pendingMu.Unlock()
		if len(ids) == 0 {
			return
		}
		job, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		rows, err := probe.Query(job, `SELECT id FROM posts WHERE id=ANY($1) AND search_data @@ plainto_tsquery('simple',content_md)`, ids)
		if err != nil {
			monitorErrors++
			emit(map[string]any{"kind": "indexProbeError", "error": err.Error()})
			return
		}
		defer rows.Close()
		now := time.Now()
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				monitorErrors++
				break
			}
			pendingMu.Lock()
			if p, ok := pending[id]; ok {
				delay := float64(now.Sub(p.Sent).Microseconds()) / 1000
				delays = append(delays, delay)
				delaysByStage[p.Stage] = append(delaysByStage[p.Stage], delay)
				delete(pending, id)
			}
			pendingMu.Unlock()
		}
		if rows.Err() != nil {
			monitorErrors++
		}
	}
	sample()
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	var monitorWG sync.WaitGroup
	monitorWG.Add(1)
	go func() {
		defer monitorWG.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		n := 0
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-tick.C:
				checkIndexes()
				n++
				if n%5 == 0 {
					sample()
				}
			}
		}
	}()
	defer func() { stopMonitor(); monitorWG.Wait() }()
	readPaths := []string{"/api/v1/home", fmt.Sprintf("/api/v1/threads?pagination=cursor&forumId=%d", fid), fmt.Sprintf("/api/v1/threads/%d/posts", tids[0]), fmt.Sprintf("/api/v1/threads/%d", tids[0]), "/api/v1/site"}
	for stage, rates := range [][2]int{{200, 5}, {400, 15}, {600, 30}} {
		pendingMu.Lock()
		currentStage = stage + 1
		pendingMu.Unlock()
		stageStart := time.Now()
		readStats, writeStats := traffic[stage][0], traffic[stage][1]
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			soakTrafficRun(ctx, duration, rates[0], 64, func(seq int) error {
				path := readPaths[seq%len(readPaths)]
				if seq%50 == 0 {
					path = fmt.Sprintf("/api/v1/search?q=content&forumId=%d", fid)
				}
				req, _ := http.NewRequestWithContext(ctx, "GET", base+path, nil)
				if seq%2 == 1 {
					req.AddCookie(&http.Cookie{Name: cookieSession, Value: people[seq%len(people)].token})
				}
				resp, err := client.Do(req)
				if err != nil {
					return fmt.Errorf("transport")
				}
				defer resp.Body.Close()
				if _, err = io.Copy(io.Discard, resp.Body); err != nil {
					return fmt.Errorf("body")
				}
				if resp.StatusCode != 200 {
					return fmt.Errorf("http_%d", resp.StatusCode)
				}
				return nil
			}, readStats)
		}()
		soakTrafficRun(ctx, duration, rates[1], 16, func(seq int) error {
			u := people[seq%len(people)]
			tid := tids[seq%len(tids)]
			if seq%5 == 0 {
				tid = tids[0]
			}
			marker := fmt.Sprintf("soakstage%dn%d", stage+1, seq)
			req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v1/threads/%d/posts", base, tid), strings.NewReader(`{"content":"`+marker+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", u.csrf)
			req.AddCookie(&http.Cookie{Name: cookieSession, Value: u.token})
			sent := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("transport")
			}
			defer resp.Body.Close()
			var body struct {
				Data struct {
					PostID  string `json:"postId"`
					Pending bool   `json:"pending"`
				} `json:"data"`
			}
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				return fmt.Errorf("body")
			}
			if resp.StatusCode != 201 {
				return fmt.Errorf("http_%d", resp.StatusCode)
			}
			id, err := strconv.ParseInt(body.Data.PostID, 10, 64)
			if err != nil || id <= 0 || body.Data.Pending {
				return fmt.Errorf("unexpected_post")
			}
			pendingMu.Lock()
			accepted++
			pending[id] = soakProof{stage + 1, sent}
			pendingMu.Unlock()
			return nil
		}, writeStats)
		<-readDone
		result := map[string]any{"kind": "stage", "stage": stage + 1, "seconds": time.Since(stageStart).Seconds(), "readTarget": rates[0], "writeTarget": rates[1], "reads": readStats.result(), "writes": writeStats.result()}
		emit(result)
		b.Logf("SOAK stage=%d reads=%v writes=%v", stage+1, readStats.result(), writeStats.result())
		if len(readStats.Errors)+len(writeStats.Errors) > 0 || readStats.Missed+writeStats.Missed > 0 {
			b.Errorf("stage %d could not sustain offered traffic", stage+1)
		}
		if ctx.Err() != nil {
			b.Fatal(ctx.Err())
		}
	}
	pendingMu.Lock()
	currentStage = 4
	pendingMu.Unlock()
	drainStart := time.Now()
	var left int64
	for {
		err = probe.QueryRow(ctx, `SELECT (SELECT count(*) FROM search_index_events)+(SELECT count(*) FROM forum_stat_events)+(SELECT count(*) FROM member_events)+(SELECT count(*) FROM title_events)+(SELECT count(*) FROM subscription_events e JOIN posts p ON p.id=e.post_id JOIN threads t ON t.id=p.thread_id WHERE NOT e.completed AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted)`).Scan(&left)
		if err != nil {
			b.Fatal(err)
		}
		pendingMu.Lock()
		missing := len(pending)
		pendingMu.Unlock()
		if left == 0 && missing == 0 {
			break
		}
		if time.Since(drainStart) > 10*time.Minute {
			b.Errorf("soak queues did not drain: %d events, %d unindexed posts", left, missing)
			break
		}
		select {
		case <-ctx.Done():
			b.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
	stopMonitor()
	monitorWG.Wait()
	checkIndexes()
	sample()
	var posts, notifications, snapshots int64
	var exact bool
	err = probe.QueryRow(ctx, `SELECT (SELECT count(*) FROM posts p JOIN threads t ON t.id=p.thread_id WHERE t.forum_id=$1),
(SELECT count(*) FROM notifications n JOIN posts p ON p.id=n.post_id JOIN threads t ON t.id=p.thread_id WHERE t.forum_id=$1 AND n.type='subscription'),
(SELECT count(DISTINCT name) FROM analytics_snapshots WHERE name IN ('site','points')),
(SELECT post_count=(SELECT count(*) FROM posts p JOIN threads t ON t.id=p.thread_id WHERE t.forum_id=$1 AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted) AND thread_count=10000 FROM forums WHERE id=$1)`, fid).Scan(&posts, &notifications, &snapshots, &exact)
	if err != nil {
		b.Fatal(err)
	}
	result := map[string]any{"kind": "final", "acceptedWrites": accepted, "posts": posts, "notifications": notifications, "snapshots": snapshots, "forumConsistent": exact, "indexedWrites": len(delays), "searchP50Ms": soakPercentile(delays, 50), "searchP95Ms": soakPercentile(delays, 95), "searchP99Ms": soakPercentile(delays, 99), "searchMaxMs": soakPercentile(delays, 100), "drainSeconds": time.Since(drainStart).Seconds(), "monitorErrors": monitorErrors}
	result["queueEventsRemaining"], result["unindexedWrites"] = left, len(pending)
	stageSearch := map[int]any{}
	for stage, v := range delaysByStage {
		stageSearch[stage] = map[string]any{"count": len(v), "p95Ms": soakPercentile(v, 95), "p99Ms": soakPercentile(v, 99), "maxMs": soakPercentile(v, 100)}
	}
	result["searchByStage"] = stageSearch
	emit(result)
	b.Logf("SOAK final=%v evidence=%s", result, path)
	if posts != 50000+accepted || notifications != accepted*20 || snapshots != 2 || !exact || int64(len(delays)) != accepted || monitorErrors != 0 {
		b.Error("soak consistency or monitoring checks failed")
	}
}

type soakProc struct{ Ticks, Read, Write, Start uint64 }
type soakResources struct {
	PID      int
	Clock    float64
	Previous map[int]soakProc
	Last     time.Time
}

func newSoakResources(b *testing.B) *soakResources {
	b.Helper()
	pid, err := strconv.Atoi(os.Getenv("FORUM_SOAK_PG_PID"))
	if err != nil || pid < 1 {
		b.Fatal("soak requires FORUM_SOAK_PG_PID from disposable cluster wrapper")
	}
	clock, err := strconv.ParseFloat(os.Getenv("FORUM_SOAK_CLOCK_TICKS"), 64)
	if err != nil || clock <= 0 {
		b.Fatal("soak requires FORUM_SOAK_CLOCK_TICKS")
	}
	return &soakResources{PID: pid, Clock: clock, Previous: map[int]soakProc{}, Last: time.Now()}
}

// Only the isolated postmaster and its children contribute to process counters.
// Physical bytes come from /proc/io; they are not PostgreSQL buffer reads.
func (r *soakResources) sample() map[string]any {
	now := time.Now()
	children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", r.PID, r.PID))
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	pids := []int{r.PID}
	for _, raw := range strings.Fields(string(children)) {
		pid, err := strconv.Atoi(raw)
		if err == nil {
			pids = append(pids, pid)
		}
	}
	var ticks, read, write uint64
	next := map[int]soakProc{}
	for _, pid := range pids {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(stat), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 20 {
			continue
		}
		u, e1 := strconv.ParseUint(fields[11], 10, 64)
		s, e2 := strconv.ParseUint(fields[12], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		started, e3 := strconv.ParseUint(fields[19], 10, 64)
		if e3 != nil {
			continue
		}
		v := soakProc{Ticks: u + s, Start: started}
		ioData, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(ioData), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if key == "read_bytes" {
				v.Read = n
			}
			if key == "write_bytes" {
				v.Write = n
			}
		}
		next[pid] = v
		if old, ok := r.Previous[pid]; ok && v.Start == old.Start && v.Ticks >= old.Ticks && v.Read >= old.Read && v.Write >= old.Write {
			ticks += v.Ticks - old.Ticks
			read += v.Read - old.Read
			write += v.Write - old.Write
		}
	}
	seconds := now.Sub(r.Last).Seconds()
	if len(next) == 0 {
		return map[string]any{"error": "no readable PostgreSQL process counters"}
	}
	r.Last = now
	r.Previous = next
	return map[string]any{"pgProcesses": len(next), "pgCPUPercentOneCore": float64(ticks) / r.Clock / seconds * 100, "pgReadBytesPerSecond": float64(read) / seconds, "pgWriteBytesPerSecond": float64(write) / seconds}
}
