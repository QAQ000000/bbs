package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dzforum/internal/config"
	"dzforum/internal/store"
)

func TestPerformanceRequestTimeout(t *testing.T) {
	s := &Server{cfg: config.Config{APIRequestTimeout: 20 * time.Millisecond}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	release, done := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		<-r.Context().Done()
		<-release
		s.respond(w, 200, "late")
	})
	r := httptest.NewRequest("GET", "/api/v1/home", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.gzipMW(s.timeoutMW(h)).ServeHTTP(w, r)
	close(release)
	<-done
	if w.Code != 503 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal(w.Code, w.Header())
	}
	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	body, err := io.ReadAll(gz)
	if err != nil || !json.Valid(body) || !strings.Contains(string(body), "REQUEST_TIMEOUT") || strings.Contains(string(body), "late") {
		t.Fatal(string(body), err)
	}
}

func TestPerformanceStreamingTimeoutExemptions(t *testing.T) {
	s := &Server{cfg: config.Config{APIRequestTimeout: time.Millisecond, UploadRequestTimeout: time.Minute}}
	for _, path := range []string{"/api/v1/events", "/uploads/file", "/api/v1/me/export", "/api/v1/uploads", "/api/v1/home"} {
		s.timeoutMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadline, ok := r.Context().Deadline()
			switch path {
			case "/api/v1/events", "/uploads/file":
				if ok {
					t.Error("stream received ordinary deadline")
				}
				if _, ok := w.(http.Flusher); !ok {
					t.Error("stream lost flushing")
				}
			case "/api/v1/me/export", "/api/v1/uploads":
				if !ok || time.Until(deadline) < 50*time.Second {
					t.Error("missing upload/export deadline")
				}
			default:
				if !ok {
					t.Error("missing API deadline")
				}
			}
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
}

func TestPerformanceTimeoutCancelsDatabaseQuery(t *testing.T) {
	if smokePool == nil {
		t.Skip("requires isolated database")
	}
	s := &Server{cfg: config.Config{APIRequestTimeout: 100 * time.Millisecond}}
	done := make(chan error, 1)
	h := s.timeoutMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := smokePool.Exec(r.Context(), `SELECT pg_sleep(5)`)
		done <- err
	}))
	start := time.Now()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/home", nil))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("query was not canceled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("query retained its connection after timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not bound query")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := smokePool.Ping(ctx); err != nil {
		t.Fatal("pool did not recover", err)
	}
}

func TestPerformanceCursorAPI(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	var fid int64
	if err := smokePool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES(1,'cursor-api') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	settings := settingsAPIFixture(t)
	if _, err := smokeSrv.st.SaveSiteSettings(ctx, settings.Version, map[string]string{"threads_per_page": "5"}, 1, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if _, err := smokePool.Exec(ctx, `INSERT INTO threads(forum_id,author_id,title,last_post_at) VALUES($1,2,'cursor','2026-01-01')`, fid); err != nil {
			t.Fatal(err)
		}
	}
	path := fmt.Sprintf("/api/v1/threads?pagination=cursor&forumId=%d", fid)
	type feedResponse struct {
		Data struct {
			Threads []struct {
				ID string `json:"id"`
			} `json:"threads"`
		} `json:"data"`
		Meta struct {
			Next string `json:"nextCursor"`
			More bool   `json:"hasMore"`
		} `json:"meta"`
	}
	first := smokeGet(t, path, nil)
	checkJSON(t, first, 200)
	var a, b feedResponse
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Data.Threads) != 5 || !a.Meta.More || a.Meta.Next == "" || strings.Contains(first.Body.String(), "totalPages") {
		t.Fatal(first.Body.String())
	}
	second := smokeGet(t, path+"&cursor="+a.Meta.Next, nil)
	checkJSON(t, second, 200)
	if err := json.Unmarshal(second.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Data.Threads) != 2 || b.Meta.More || b.Meta.Next != "" {
		t.Fatal(second.Body.String())
	}
	seen := map[string]bool{}
	for _, v := range append(a.Data.Threads, b.Data.Threads...) {
		if seen[v.ID] {
			t.Fatal("duplicate cursor item")
		}
		seen[v.ID] = true
	}
	checkJSON(t, smokeGet(t, path+"&cursor=bad", nil), 422)
	checkJSON(t, smokeGet(t, path+"&page=1", nil), 422)
	checkJSON(t, smokeGet(t, path+"&sort=hot", nil), 422)
	checkJSON(t, smokeGet(t, "/api/v1/threads?cursor="+a.Meta.Next, nil), 422)
	checkJSON(t, smokeGet(t, fmt.Sprintf("/api/v1/threads?forumId=%d&page=1", fid), nil), 200)

	// Permission changes must apply even when continuing a previously issued cursor.
	c, err := smokeSrv.st.MembershipConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(c)
	t.Cleanup(func() {
		_, err := smokePool.Exec(ctx, `UPDATE membership_config SET body=$1 WHERE id`, original)
		if err != nil {
			t.Error(err)
		}
	})
	c.Forums = append(c.Forums, store.ForumMembership{ForumID: fid, MinimumLevel: 0, MembersOnly: true, Denied: []string{}})
	body, _ := json.Marshal(c)
	if _, err := smokePool.Exec(ctx, `UPDATE membership_config SET body=$1 WHERE id`, body); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, path+"&cursor="+a.Meta.Next, nil), 404)
}

func TestPerformanceMembershipRevalidation(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	r, err := smokeSrv.loadMembership(httptest.NewRequest("GET", "/api/v1/home", nil))
	if err != nil {
		t.Fatal(err)
	}
	c := membershipOf(r).Config
	original, _ := json.Marshal(c)
	t.Cleanup(func() {
		if _, err := smokePool.Exec(ctx, `UPDATE membership_config SET body=$1 WHERE id`, original); err != nil {
			t.Error(err)
		}
	})
	var changed store.MembershipConfig
	if err := json.Unmarshal(original, &changed); err != nil {
		t.Fatal(err)
	}
	changed.GuestPermissions["forum.read"] = false
	body, _ := json.Marshal(changed)
	if _, err := smokePool.Exec(ctx, `UPDATE membership_config SET body=$1 WHERE id`, body); err != nil {
		t.Fatal(err)
	}
	fresh, err := smokeSrv.loadMembership(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range membershipOf(fresh).Forums {
		if allowed {
			t.Fatal("revalidation retained stale permissions")
		}
	}
}
