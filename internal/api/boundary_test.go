package api

import (
	"dzforum/internal/config"
	"dzforum/internal/live"
	"dzforum/internal/store"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBackendHasNoPageRoutes(t *testing.T) {
	s, err := New(config.Config{}, nil, live.NewHub(), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/login", "/register", "/admin", "/forum-1-1.html", "/thread-1-1-1.html", "/static/css/app.css", "/favicon.ico", "/rss", "/sitemap.xml", "/robots.txt", "/api/preview"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		checkJSON(t, w, 404)
		if strings.Contains(w.Body.String(), "<html") {
			t.Fatal("rendered HTML")
		}
	}
}
func TestDTOExcludesSecretsAndKeepsLargeIDs(t *testing.T) {
	u := &store.User{ID: 9007199254740993, Username: "user", Email: "private@example.org", PasswordHash: "secret-password-hash"}
	for _, v := range []any{publicUser(u), privateUser(u)} {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "secret-password-hash") {
			t.Fatal("password leaked")
		}
		if !strings.Contains(string(b), `"id":"9007199254740993"`) {
			t.Fatal("ID lost precision")
		}
	}
	b, _ := json.Marshal(publicUser(u))
	if strings.Contains(string(b), u.Email) {
		t.Fatal("email leaked")
	}
	b, _ = json.Marshal(postDTO(&store.Post{ContentMD: "**original**", ContentHTML: "<p>legacy</p>", IP: "192.0.2.123"}))
	for _, bad := range []string{"legacy", "192.0.2.123", "ContentHTML"} {
		if strings.Contains(string(b), bad) {
			t.Fatal("post leaked", bad)
		}
	}
}
func TestActionJSONValidation(t *testing.T) {
	s := &Server{log: slogNop()}
	token := strings.Repeat("c", 24)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"ids":["9007199254740993","2"],"enabled":true}`, 200},
		{`null`, 400}, {`[]`, 400}, {`{} {}`, 400}, {`{"object":{}}`, 400}, {`{"value":null}`, 400}, {`{"value":"` + strings.Repeat("x", (1<<20)) + `"}`, 413},
	} {
		called := false
		h := s.action(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if r.PostFormValue("ids") != "9007199254740993" || r.PostFormValue("enabled") != "1" {
				t.Fatal("JSON changed form semantics")
			}
			s.respond(w, 200, map[string]bool{"ok": true})
		})
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
		r.AddCookie(&http.Cookie{Name: cookieCSRF, Value: token})
		w := httptest.NewRecorder()
		h(w, r)
		checkJSON(t, w, tc.status)
		if called != (tc.status == 200) {
			t.Fatal("invalid body reached action")
		}
	}
}
func TestInternalErrorsAreJSONAndRedacted(t *testing.T) {
	s := &Server{log: slogNop()}
	w := httptest.NewRecorder()
	s.fail(w, httptest.NewRequest("GET", "/api/v1/site", nil), 500, "查询失败", "postgres://user:secret@host/db SQL SELECT password_hash")
	checkJSON(t, w, 500)
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "SQL") {
		t.Fatal("internal details leaked")
	}
}
func TestSSEPublishesOnlyData(t *testing.T) {
	s := &Server{hub: live.NewHub()}
	sub, cancel := s.hub.Subscribe(4, "t:1")
	defer cancel()
	s.broadcastPost("post.edit", &store.Thread{ID: 1, ForumID: 2}, &store.Post{ID: 3, Version: 4, ContentMD: "private raw", ContentHTML: "<p>private html</p>"})
	select {
	case b := <-sub.C():
		var v map[string]any
		if json.Unmarshal(b, &v) != nil {
			t.Fatal("invalid event")
		}
		if v["postId"] != "3" || v["version"] != float64(4) {
			t.Fatal("missing resource version", v)
		}
		for _, bad := range []string{"private", "postHtml", "threadRow", "forumRow"} {
			if strings.Contains(string(b), bad) {
				t.Fatal("HTML in event")
			}
		}
	default:
		t.Fatal("no event")
	}
}
