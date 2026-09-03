// SPDX-License-Identifier: AGPL-3.0-or-later

// 整站渲染冒烟测试：对独立测试库（forum_test_web）驱动全部 GET 路由，
// 断言每个页面为 200 且完整渲染（含 </html> 收尾、无错误页文案）。
// 该测试是“模板引用字段缺失导致响应截断”（P0）与“列清单与 Scan 不同步导致 500”
// （P1）两类事故的回归防线。数据库不可达时自动跳过。

package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("FORUM_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://123456:123456@127.0.0.1:5432/forum_test_web"
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		fmt.Println("SKIP: 冒烟测试数据库不可达（", err, "）")
		os.Exit(0)
	}
	smokePool = pool
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		fmt.Println("SKIP: 无法重置冒烟测试库:", err)
		os.Exit(0)
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
	th2, p2, err := st.CreateThread(ctx, fid, user.ID, user.Username, "待回收主题", "x", "<p>x</p>", false, "")
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	if _, _, err := st.DeletePost(ctx, p2.ID); err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	_ = th2
	_ = st.SaveAnnouncement(ctx, admin.ID, admin.Username, "冒烟测试公告")
	_ = st.AddCensorWord(ctx, "敏感词测试", "***")

	// 会话 Cookie
	tokA, _, err := st.CreateSession(ctx, admin.ID)
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	tokU, _, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
	adminCookie = &http.Cookie{Name: "forum_session", Value: tokA}
	userCookie = &http.Cookie{Name: "forum_session", Value: tokU}
	cfg := config.FromEnv()
	cfg.SiteName = "GoBBS 冒烟站"
	hub := live.NewHub()
	srv, err := New(cfg, st, hub, slogNop())
	if err != nil {
		fmt.Println("FATAL: 初始化失败:", err)
		os.Exit(1)
	}
	smokeSrv = srv

	code := m.Run()
	smokePool.Close()
	os.Exit(code)
}

func slogNop() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func smokeGet(t *testing.T, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(w, req)
	return w
}

// TestPageSmoke 逐页断言：200、完整收尾、无错误页文案、最小长度。
func TestPageSmoke(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		cookie *http.Cookie
		minLen int
	}{
		{"首页", "/", nil, 2500},
		{"版块页", "/forum-1-1.html", nil, 2000},
		{"帖子页", "/thread-1-1-1.html", nil, 2500},
		{"搜索页", "/search?q=%E5%86%85%E5%AE%B9", nil, 1500},
		{"登录页", "/login", nil, 1500},
		{"注册页", "/register", nil, 2000},
		{"个人空间", "/user/2", nil, 1500},
		{"发帖表单", "/new?fid=1", userCookie, 2500},
		{"回复表单", "/reply/1", userCookie, 2500},
		{"编辑表单", "/edit/1", userCookie, 2500},
		{"通知页", "/notify", userCookie, 1200},
		{"后台仪表盘", "/admin", adminCookie, 1800},
		{"版块管理", "/admin/forums", adminCookie, 2000},
		{"内容管理", "/admin/threads", adminCookie, 1800},
		{"用户管理", "/admin/users", adminCookie, 2200},
		{"站点设置", "/admin/settings", adminCookie, 1600},
		{"管理日志", "/admin/logs", adminCookie, 1000},
		{"回收站(含软删数据)", "/admin/recyclebin", adminCookie, 1800},
		{"敏感词", "/admin/censor", adminCookie, 1200},
		{"公告管理", "/admin/announcements", adminCookie, 1300},
		{"审核队列", "/admin/moderate", adminCookie, 1300},
		{"批量删帖", "/admin/prune", adminCookie, 1600},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := smokeGet(t, c.path, c.cookie)
			body := w.Body.String()
			if w.Code != http.StatusOK {
				t.Fatalf("状态码 %d（期望 200）: %s", w.Code, firstLine(body))
			}
			if !strings.Contains(body, "</html>") {
				t.Fatalf("响应未完整渲染（缺少 </html>），长度 %d —— 疑似模板字段缺失截断", len(body))
			}
			if len(body) < c.minLen {
				t.Fatalf("响应过短 %dB（期望 ≥%dB），疑似渲染不完整: %s", len(body), c.minLen, firstLine(body))
			}
			for _, bad := range []string{"加载失败", "操作被拒绝", "无权访问"} {
				if strings.Contains(body, bad) {
					t.Fatalf("页面出现错误文案 %q", bad)
				}
			}
		})
	}
}

// TestStaticAssets 静态资源可访问且带缓存头。
func TestStaticAssets(t *testing.T) {
	w := smokeGet(t, "/static/css/app.css", nil)
	if w.Code != 200 {
		t.Fatalf("app.css: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Fatal("静态资源缺少 Cache-Control")
	}
}

// TestAPIJSON JSON 端点基本可用。
func TestAPIJSON(t *testing.T) {
	w := smokeGet(t, "/api/status", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("/api/status 异常: %d %s", w.Code, w.Body.String())
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
