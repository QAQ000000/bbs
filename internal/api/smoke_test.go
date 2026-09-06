// SPDX-License-Identifier: AGPL-3.0-or-later
// API integration tests. Only explicit disposable databases may be reset.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"dzforum/internal/config"
	"dzforum/internal/db"
	"dzforum/internal/live"
	"dzforum/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	smokeSrv    *Server
	smokePool   *pgxpool.Pool
	adminCookie *http.Cookie
	userCookie  *http.Cookie
	modCookie   *http.Cookie
	adminCSRF   string
	userCSRF    string
	modCSRF     string
	smokeFid2   int64
	smokeTid2   int64
	smokePid2   int64 // 版块二主题的首楼 id（版主管辖回归用）
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("FORUM_TEST_DSN")
	if dsn == "" {
		os.Exit(m.Run())
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(parsed.ConnConfig.Database, "gobbs_test_") {
		fmt.Fprintln(os.Stderr, "FORUM_TEST_DSN must point to a disposable gobbs_test_ database")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		fmt.Println("SKIP: 冒烟测试数据库不可达（", err, "）")
		os.Exit(1)
	}
	smokePool = pool
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		fmt.Println("SKIP: 无法重置冒烟测试库:", err)
		os.Exit(1)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		fmt.Println("FATAL: 迁移失败:", err)
		os.Exit(1)
	}
	st := store.New(pool)

	// 最小数据集：管理员 + 普通用户 + 分类/版块 + 主题/回复 + 软删主题 + 公告 + 敏感词
	admin, err := st.CreateUser(ctx, "admin", "admin123456", "admin@test.local")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET group_id=1 WHERE id=$1`, admin.ID); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	user, err := st.CreateUser(ctx, "user01", "user123456", "user@test.local")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	// mod01 先建（SaveForum 同步 forum_moderators 需要）
	mod, err := st.CreateUser(ctx, "mod01", "mod123456", "mod@test.local")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET group_id=2 WHERE id=$1`, mod.ID); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	var fid int64
	if err := pool.QueryRow(ctx, `INSERT INTO categories (name) VALUES ('冒烟分类') RETURNING id`).Scan(new(int)); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO forums (category_id, name, description, moderators) VALUES (1,'冒烟版块','冒烟测试版块','admin') RETURNING id`).Scan(&fid); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if _, err := st.SaveForum(ctx, fid, 1, "冒烟版块", "冒烟测试版块", "mod01"); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	// 主题 1：后续 /thread-1-1-1.html 等硬编码断言依赖此顺序
	th, _, err := st.CreateThread(ctx, fid, user.ID, user.Username, "冒烟测试主题", "首楼内容 :smile:", "<p>首楼内容</p>", false, "")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if _, _, err := st.CreateReply(ctx, th.ID, admin.ID, admin.Username, "回复内容", "<p>回复内容</p>", false, ""); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	// 软删主题：回收站列表页必须有数据才有回归价值
	thR, pR, err := st.CreateThread(ctx, fid, user.ID, user.Username, "待回收主题", "x", "<p>x</p>", false, "")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if _, _, err := st.DeletePost(ctx, pR.ID); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	_ = thR
	// 版主管辖回归用：版块二（无版主）+ 其主题；建在主线种子之后，不干扰 /thread-1-1-1.html
	if err := pool.QueryRow(ctx,
		`INSERT INTO forums (category_id, name, description) VALUES (1,'冒烟版块二','无版主管辖') RETURNING id`).Scan(&smokeFid2); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	th2, p2, err := st.CreateThread(ctx, smokeFid2, user.ID, user.Username, "版块二主题", "版块二首楼", "<p>版块二首楼</p>", false, "")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	smokeTid2, smokePid2 = th2.ID, p2.ID
	_ = st.SaveAnnouncement(ctx, admin.ID, admin.Username, "冒烟测试公告")
	_ = st.AddCensorWord(ctx, "敏感词测试", "***")

	// 会话 Cookie
	tokA, csrfA, err := st.CreateSession(ctx, admin.ID)
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	tokU, csrfU, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	tokM, csrfM, err := st.CreateSession(ctx, mod.ID)
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	adminCookie = &http.Cookie{Name: "forum_session", Value: tokA}
	userCookie = &http.Cookie{Name: "forum_session", Value: tokU}
	modCookie = &http.Cookie{Name: "forum_session", Value: tokM}
	adminCSRF, userCSRF, modCSRF = csrfA, csrfU, csrfM
	cfg := config.FromEnv()
	cfg.SiteName = "GoBBS 冒烟站"
	uploadDir, err := os.MkdirTemp("", "gobbs-api-uploads-")
	if err != nil {
		panic(err)
	}
	cfg.UploadDir = uploadDir
	hub := live.NewHub()
	srv, err := New(cfg, st, hub, slogNop())
	if err != nil {
		fmt.Println("FATAL: 初始化失败:", err)
		os.Exit(1)
	}
	smokeSrv = srv

	code := m.Run()
	smokePool.Close()
	os.RemoveAll(uploadDir)
	os.Exit(code)
}

func slogNop() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func smokeGet(t *testing.T, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	requireDB(t)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(w, req)
	return w
}

// smokePost 以指定会话提交表单（csrf 拼入表单体）。
func smokePost(t *testing.T, path, csrf, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("_csrf="+csrf+"&"+body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(w, req)
	return w
}

func smokeMultipart(t *testing.T, path, csrf, fileField, filename string, content []byte, extra map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fileField != "" {
		fw, _ := w.CreateFormFile(fileField, filename)
		_, _ = fw.Write(content)
	}
	for k, v := range extra {
		_ = w.WriteField(k, v)
	}
	_ = w.WriteField("_csrf", csrf)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(resp, req)
	return resp
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

var pdfMagic = []byte("%PDF-1.4\n%test\n")

func requireDB(t *testing.T) {
	t.Helper()
	if smokeSrv == nil {
		t.Skip("set FORUM_TEST_DSN to an isolated gobbs_test_ database for integration tests")
	}
}
func apiRequest(t *testing.T, method, path, csrf, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	requireDB(t)
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(w, r)
	return w
}
func checkJSON(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]json.RawMessage {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP %d want %d: %s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Location") != "" {
		t.Fatal("API redirected")
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("not JSON:", w.Header())
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func firstLine(s string) string {
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
func smokePatch(t *testing.T, path, csrf, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	requireDB(t)
	r := httptest.NewRequest(http.MethodPatch, path, strings.NewReader("_csrf="+csrf+"&"+body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(w, r)
	return w
}

func TestReadAPISmoke(t *testing.T) {
	for _, path := range []string{"/api/v1/site", "/api/v1/home", "/api/v1/forums", "/api/v1/forums/1", "/api/v1/threads", "/api/v1/threads?forumId=1&sort=digest", "/api/v1/threads/1", "/api/v1/threads/1/posts", "/api/v1/posts/1", "/api/v1/search?q=内容", "/api/v1/users/2", "/api/v1/users/2?tab=replies", "/api/v1/smileys", "/api/v1/setup"} {
		t.Run(path, func(t *testing.T) {
			w := smokeGet(t, path, nil)
			checkJSON(t, w, 200)
			for _, bad := range []string{"passwordHash", "PasswordHash", "contentHtml", "ContentHTML", "user@test.local", "<html"} {
				if strings.Contains(w.Body.String(), bad) {
					t.Fatalf("leaked %s", bad)
				}
			}
		})
	}
	for _, path := range []string{"/api/v1/me", "/api/v1/me/favorites", "/api/v1/me/drafts", "/api/v1/me/notifications", "/api/v1/posts/1/history"} {
		checkJSON(t, smokeGet(t, path, userCookie), 200)
		checkJSON(t, smokeGet(t, path, nil), 401)
	}
	for _, path := range []string{"/api/v1/admin", "/api/v1/admin/forums", "/api/v1/admin/threads", "/api/v1/admin/users", "/api/v1/admin/settings", "/api/v1/admin/perms", "/api/v1/admin/logs", "/api/v1/admin/recyclebin", "/api/v1/admin/censor", "/api/v1/admin/announcements", "/api/v1/admin/moderate"} {
		checkJSON(t, smokeGet(t, path, adminCookie), 200)
		checkJSON(t, smokeGet(t, path, userCookie), 403)
	}
}
func TestCapabilitiesAndModeratorScope(t *testing.T) {
	cases := []struct {
		pid             string
		cookie          *http.Cookie
		edit, del, like bool
	}{{"1", nil, false, false, false}, {"1", userCookie, true, true, false}, {"2", userCookie, false, false, true}, {"2", modCookie, false, true, true}, {idString(smokePid2), modCookie, false, false, true}, {idString(smokePid2), adminCookie, true, true, true}}
	for _, c := range cases {
		raw := checkJSON(t, smokeGet(t, "/api/v1/posts/"+c.pid, c.cookie), 200)
		var v struct {
			Capabilities struct{ CanEdit, CanDelete, CanLike bool }
		}
		if err := json.Unmarshal(raw["data"], &v); err != nil {
			t.Fatal(err)
		}
		if v.Capabilities.CanEdit != c.edit || v.Capabilities.CanDelete != c.del || v.Capabilities.CanLike != c.like {
			t.Fatalf("unexpected capabilities: %+v", v)
		}
	}
	checkJSON(t, apiRequest(t, "DELETE", "/api/v1/posts/"+idString(smokePid2), modCSRF, "{}", modCookie), 403)
}
func TestAuthJSONAndCSRF(t *testing.T) {
	w := smokeGet(t, "/api/v1/session", nil)
	v := checkJSON(t, w, 200)
	var sess struct{ CSRFToken string }
	json.Unmarshal(v["data"], &sess)
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieCSRF {
			cookie = c
		}
	}
	if cookie == nil || sess.CSRFToken == "" {
		t.Fatal("missing anonymous CSRF")
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/login", "wrong", `{"username":"user01","password":"user123456"}`, cookie), 403)
	login := apiRequest(t, "POST", "/api/v1/auth/login", sess.CSRFToken, `{"username":"user01","password":"user123456"}`, cookie)
	checkJSON(t, login, 200)
	var logged *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == cookieSession {
			logged = c
			if !c.HttpOnly {
				t.Fatal("session not HttpOnly")
			}
		}
	}
	if logged == nil {
		t.Fatal("login did not set session")
	}
	me := checkJSON(t, smokeGet(t, "/api/v1/session", logged), 200)
	json.Unmarshal(me["data"], &sess)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/logout", "", `{}`, logged), 403)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/logout", sess.CSRFToken, `{}`, logged), 200)
	checkJSON(t, smokeGet(t, "/api/v1/me", logged), 401)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/login", cookie.Value, `{"username":"user01","password":"wrong"}`, cookie), 401)
}
func TestThreadWriteLifecycle(t *testing.T) {
	v := checkJSON(t, apiRequest(t, "POST", "/api/v1/threads", userCSRF, `{"forumId":"1","subject":"API生命周期","content":"原始 **Markdown** 内容"}`, userCookie), 201)
	var created struct {
		ThreadID, PostID string
		Version          int
	}
	json.Unmarshal(v["data"], &created)
	if created.ThreadID == "" || created.PostID == "" {
		t.Fatal("missing IDs")
	}
	var storedHTML string
	if err := smokePool.QueryRow(context.Background(), "SELECT content_html FROM posts WHERE id=$1", created.PostID).Scan(&storedHTML); err != nil {
		t.Fatal(err)
	}
	if storedHTML != "" {
		t.Fatal("backend still rendered Markdown")
	}
	checkJSON(t, apiRequest(t, "PATCH", "/api/v1/posts/"+created.PostID, userCSRF, `{"subject":"API生命周期","content":"编辑内容","version":999}`, userCookie), 409)
	checkJSON(t, apiRequest(t, "PATCH", "/api/v1/posts/"+created.PostID, userCSRF, `{"subject":"API生命周期","content":"编辑内容","version":1}`, userCookie), 200)
	h := smokeGet(t, "/api/v1/posts/"+created.PostID+"/history", userCookie)
	checkJSON(t, h, 200)
	if !strings.Contains(h.Body.String(), "Markdown") {
		t.Fatal("missing edit snapshot")
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/"+created.ThreadID+"/posts", adminCSRF, `{"content":"回复通知回归内容"}`, adminCookie), 201)
	n := smokeGet(t, "/api/v1/me/notifications", userCookie)
	checkJSON(t, n, 200)
	if !strings.Contains(n.Body.String(), "回复通知回归内容") {
		t.Fatal("missing reply notification")
	}
	checkJSON(t, apiRequest(t, "DELETE", "/api/v1/posts/"+created.PostID, userCSRF, `{}`, userCookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/threads/"+created.ThreadID, nil), 404)
}
func TestDraftFavoriteAndRead(t *testing.T) {
	checkJSON(t, apiRequest(t, "POST", "/api/v1/me/draft", userCSRF, `{"context":"reply:1","content":"草稿箱回归内容"}`, userCookie), 200)
	w := smokeGet(t, "/api/v1/me/drafts", userCookie)
	checkJSON(t, w, 200)
	if !strings.Contains(w.Body.String(), "草稿箱回归内容") {
		t.Fatal("missing draft")
	}
	checkJSON(t, apiRequest(t, "DELETE", "/api/v1/me/draft", userCSRF, `{"context":"reply:1"}`, userCookie), 200)
	for _, want := range []bool{true, false} {
		v := checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/1/favorite", userCSRF, `{}`, userCookie), 200)
		var r struct{ Favorite bool }
		json.Unmarshal(v["data"], &r)
		if r.Favorite != want {
			t.Fatal("favorite state", r)
		}
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/1/read", userCSRF, `{"postId":"2"}`, userCookie), 200)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/1/read", userCSRF, `{"postId":"`+idString(smokePid2)+`"}`, userCookie), 404)
}
func TestVisibilityAcrossReadsAndActions(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	th, p, err := smokeSrv.st.CreateThread(ctx, smokeFid2, 2, "user01", "隐藏主题", "隐藏原文", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*http.Cookie{nil, modCookie} {
		checkJSON(t, smokeGet(t, "/api/v1/posts/"+idString(p.ID), c), 404)
		checkJSON(t, smokeGet(t, "/api/v1/threads/"+idString(th.ID)+"/posts", c), 404)
	}
	checkJSON(t, smokeGet(t, "/api/v1/posts/"+idString(p.ID), userCookie), 200)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/"+idString(th.ID)+"/posts", modCSRF, `{"content":"不应成功"}`, modCookie), 404)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/posts/"+idString(p.ID)+"/like", modCSRF, `{}`, modCookie), 404)
	// A public thread's owner must not see other authors' pending replies.
	_, reply, err := smokeSrv.st.CreateReply(ctx, 1, 1, "admin", "另一个作者的待审回复", "", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	w := smokeGet(t, "/api/v1/threads/1/posts", userCookie)
	checkJSON(t, w, 200)
	if strings.Contains(w.Body.String(), "另一个作者的待审回复") {
		t.Fatal("thread owner sees others' pending reply")
	}
	checkJSON(t, smokeGet(t, "/api/v1/posts/"+idString(reply.ID), userCookie), 404)
}
func TestProfilePasswordAndExport(t *testing.T) {
	checkJSON(t, apiRequest(t, "PATCH", "/api/v1/me", userCSRF, `{"signature":"API签名","email":"user@test.local"}`, userCookie), 200)
	w := smokeGet(t, "/api/v1/users/2", nil)
	if !strings.Contains(w.Body.String(), "API签名") || strings.Contains(w.Body.String(), "user@test.local") {
		t.Fatal("public profile projection")
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/me/password", userCSRF, `{"old_password":"wrong","new_password":"newpass123","confirm_password":"newpass123"}`, userCookie), 422)
	for _, body := range []string{`{"old_password":"user123456","new_password":"newpass123","confirm_password":"newpass123"}`, `{"old_password":"newpass123","new_password":"user123456","confirm_password":"user123456"}`} {
		checkJSON(t, apiRequest(t, "POST", "/api/v1/me/password", userCSRF, body, userCookie), 200)
	}
	w = smokeGet(t, "/api/v1/me/export", userCookie)
	checkJSON(t, w, 200)
	if strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("export leaked password")
	}
}
func TestMustChangePasswordGate(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	if _, err := smokePool.Exec(ctx, "UPDATE users SET must_change_password=true WHERE id IN (1,2)"); err != nil {
		t.Fatal(err)
	}
	defer smokePool.Exec(ctx, "UPDATE users SET must_change_password=false WHERE id IN (1,2)")
	checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/1/posts", userCSRF, `{"content":"不能发帖"}`, userCookie), 403)
	checkJSON(t, smokeGet(t, "/api/v1/admin", adminCookie), 403)
	w := smokeGet(t, "/api/v1/me", userCookie)
	checkJSON(t, w, 200)
	if !strings.Contains(w.Body.String(), `"mustChangePassword":true`) {
		t.Fatal("missing change-password state")
	}
}
func TestUploadsAuthorization(t *testing.T) {
	w := smokeMultipart(t, "/api/v1/uploads", userCSRF, "file", "report.pdf", pdfMagic, map[string]string{"kind": "file"}, userCookie)
	v := checkJSON(t, w, 201)
	var uploaded struct{ URL string }
	json.Unmarshal(v["data"], &uploaded)
	if uploaded.URL == "" {
		t.Fatal("missing upload URL")
	}
	checkStatus := func(path string, c *http.Cookie, status int) {
		t.Helper()
		w := smokeGet(t, path, c)
		if w.Code != status {
			t.Fatalf("file %s: %d want %d", path, w.Code, status)
		}
		if w.Header().Get("Cache-Control") != "no-store" && status == 200 {
			t.Fatal("controlled file is shared-cacheable")
		}
	}
	checkStatus(uploaded.URL, nil, 404)
	checkStatus(uploaded.URL, userCookie, 200)
	v = checkJSON(t, apiRequest(t, "POST", "/api/v1/threads/"+idString(smokeTid2)+"/posts", userCSRF, `{"content":"[附件](`+uploaded.URL+`)"}`, userCookie), 201)
	var reply struct{ PostID string }
	json.Unmarshal(v["data"], &reply)
	// Explicitly put this reply into moderation independently of membership growth.
	pendingPID, _ := strconv.ParseInt(reply.PostID, 10, 64)
	if err := smokeSrv.st.SetPostPendingModeration(context.Background(), pendingPID, "manual"); err != nil {
		t.Fatal(err)
	}
	checkStatus(uploaded.URL, modCookie, 404)
	checkStatus(uploaded.URL, adminCookie, 200)
	pid, _ := strconv.ParseInt(reply.PostID, 10, 64)
	if _, _, err := smokeSrv.st.SetPostApproved(context.Background(), pid); err != nil {
		t.Fatal(err)
	}
	checkStatus(uploaded.URL, nil, 200)
	if err := smokeSrv.st.SetPostPendingModeration(context.Background(), pid, "manual"); err != nil {
		t.Fatal(err)
	}
	checkStatus(uploaded.URL, nil, 404)
	for _, path := range []string{"/uploads/", "/uploads/2026/", "/uploads/missing.pdf"} {
		checkStatus(path, nil, 404)
	}
}
func TestReportsAndGovernance(t *testing.T) {
	checkJSON(t, apiRequest(t, "POST", "/api/v1/posts/2/reports", userCSRF, `{"reason":"API举报回归"}`, userCookie), 200)
	w := smokeGet(t, "/api/v1/admin/moderate", adminCookie)
	checkJSON(t, w, 200)
	if !strings.Contains(w.Body.String(), "API举报回归") {
		t.Fatal("missing report")
	}
	var rid int64
	if err := smokePool.QueryRow(context.Background(), "SELECT id FROM reports WHERE reason='API举报回归'").Scan(&rid); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/admin/report/handle", adminCSRF, `{"id":"`+idString(rid)+`","op":"dismiss"}`, adminCookie), 200)
	for _, path := range []string{"/api/v1/admin/users/ban", "/api/v1/admin/forums/save", "/api/v1/admin/perms/save", "/api/v1/admin/recyclebin/purgeall"} {
		checkJSON(t, apiRequest(t, "POST", path, userCSRF, `{}`, userCookie), 403)
	}
}

func TestRegistrationResetAndEmailVerification(t *testing.T) {
	w := smokeGet(t, "/api/v1/session", nil)
	checkJSON(t, w, 200)
	var anon *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieCSRF {
			anon = c
		}
	}
	if anon == nil {
		t.Fatal("missing csrf cookie")
	}
	body := `{"username":"api_new_member","password":"newuser123456","email":"new@example.org","consent":true}`
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/register", anon.Value, `{"username":"api_no_consent","password":"newuser123456"}`, anon), 422)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/register", anon.Value, body, anon), 200)
	u, err := smokeSrv.st.UserByName(context.Background(), "api_new_member")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := smokeSrv.st.CreateEmailVerify(context.Background(), u.ID, u.Email)
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/email/verify", anon.Value, `{"token":"`+raw+`"}`, anon), 200)
	u, err = smokeSrv.st.UserByID(context.Background(), u.ID)
	if err != nil || !u.EmailVerified {
		t.Fatal("email verification did not persist", err)
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/email/verify", anon.Value, `{"token":"`+raw+`"}`, anon), 400)
	reset, err := smokeSrv.st.CreatePasswordReset(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/password/reset", anon.Value, `{"token":"`+reset+`","password":"changed123456"}`, anon), 200)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/login", anon.Value, `{"username":"api_new_member","password":"changed123456"}`, anon), 200)
	checkJSON(t, apiRequest(t, "POST", "/api/v1/auth/password/reset", anon.Value, `{"token":"`+reset+`","password":"changed987654"}`, anon), 400)
}
func TestGETDoesNotCreditReadingOrMarkNotifications(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	var before, after int64
	if err := smokePool.QueryRow(ctx, "SELECT posts_read FROM users WHERE id=2").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		checkJSON(t, smokeGet(t, "/api/v1/threads/1/posts", userCookie), 200)
	}
	if err := smokePool.QueryRow(ctx, "SELECT posts_read FROM users WHERE id=2").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("GET counted reading")
	}
	before = smokeSrv.st.UnreadCount(ctx, 2)
	checkJSON(t, smokeGet(t, "/api/v1/me/notifications", userCookie), 200)
	if got := smokeSrv.st.UnreadCount(ctx, 2); got != before {
		t.Fatal("GET marked notifications read")
	}
}
func TestSSERevalidatesRevokedSessions(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	u, err := smokeSrv.st.CreateUser(ctx, "sse_reader", "reader123456", "")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := smokeSrv.st.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/events?thread=1", nil)
	r.AddCookie(&http.Cookie{Name: cookieSession, Value: tok})
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
	if !smokeSrv.liveAuthorized(r) {
		t.Fatal("valid subscriber rejected")
	}
	smokeSrv.st.DeleteSession(ctx, tok)
	if smokeSrv.liveAuthorized(r) {
		t.Fatal("revoked session still authorized")
	}
}
