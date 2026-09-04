// SPDX-License-Identifier: AGPL-3.0-or-later

// 整站渲染冒烟测试：对独立测试库（forum_test_web）驱动全部 GET 路由，
// 断言每个页面为 200 且完整渲染（含 </html> 收尾、无错误页文案）。
// 该测试是“模板引用字段缺失导致响应截断”（P0）与“列清单与 Scan 不同步导致 500”
// （P1）两类事故的回归防线。数据库不可达时自动跳过。

package web

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
		{"最新回复页", "/latest", nil, 2000},
		{"版块页", "/forum-1-1.html", nil, 2000},
		{"帖子页", "/thread-1-1-1.html", nil, 2500},
		{"搜索页", "/search?q=%E5%86%85%E5%AE%B9", nil, 1500},
		{"登录页", "/login", nil, 1500},
		{"忘记密码页", "/forgot", nil, 1500},
		{"重置页(无效令牌走错误页)", "/reset?token=invalid", nil, 800},
		{"注册页", "/register", nil, 2000},
		{"服务条款", "/terms", nil, 1200},
		{"隐私政策", "/privacy", nil, 1200},
		{"个人空间", "/user/2", nil, 1500},
		{"发帖选版块(无fid)", "/new", userCookie, 1200},
		{"发帖表单", "/new?fid=1", userCookie, 2500},
		{"回复表单", "/reply/1", userCookie, 2500},
		{"编辑表单", "/edit/1", userCookie, 2500},
		{"通知页", "/notify", userCookie, 1200},
		{"资料设置", "/profile", userCookie, 1800},
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
			if c.name == "重置页(无效令牌走错误页)" {
				if w.Code != http.StatusBadRequest || !strings.Contains(body, "链接无效") {
					t.Fatalf("无效令牌应 400+提示: %d %s", w.Code, firstLine(body))
				}
				return
			}
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

// TestLikeButtonVisibility 楼层操作区按身份的可见性回归
// （曾发生：点赞按钮被包进 {{if .Editable}}，普通用户看不到任何点赞按钮）。
// 冒烟数据：主题 1 首楼由 user01 发、回复由 admin 发。
func TestLikeButtonVisibility(t *testing.T) {
	thread := "/thread-1-1-1.html"

	// 匿名：无点赞按钮，但点赞计数可见
	body := smokeGet(t, thread, nil).Body.String()
	if strings.Contains(body, `class="linklike likebtn"`) {
		t.Fatal("匿名用户不应看到点赞按钮")
	}
	if !strings.Contains(body, `data-lc="`) {
		t.Fatal("匿名用户应看到点赞计数（浮层入口）")
	}

	// 普通用户：别人的楼层有按钮（首楼是 user01 自己的 → 无按钮；admin 的回复 → 有按钮）
	body = smokeGet(t, thread, userCookie).Body.String()
	if !strings.Contains(body, `class="linklike likebtn"`) {
		t.Fatal("普通用户应在他人的楼层看到点赞按钮")
	}
	ownBlock := strings.SplitN(strings.SplitN(body, `id="post1"`, 2)[1], `id="post2"`, 2)[0]
	if strings.Contains(ownBlock, `likebtn`) {
		t.Fatal("自己的楼层不应出现点赞按钮")
	}

	// 管理员：所有他人楼层均有按钮
	body = smokeGet(t, thread, adminCookie).Body.String()
	if !strings.Contains(body, `class="linklike likebtn"`) {
		t.Fatal("管理员应看到点赞按钮")
	}
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

// TestAdminPostAuthorization 后台写操作授权回归。
// 事故背景：POST 操作曾只校验 CSRF 无权限守卫，匿名可禁言管理员/篡改站点设置。
func TestAdminPostAuthorization(t *testing.T) {
	// 匿名会话（持有效匿名 CSRF Cookie —— 攻击者可自取）
	w := smokeGet(t, "/register", nil)
	var anonCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "forum_csrf" {
			anonCookie = c
		}
	}
	anonCSRF := ""
	if anonCookie != nil {
		anonCSRF = anonCookie.Value
	}

	adminOnlyOps := []struct{ path, body string }{
		{"/admin/settings", "site_name=hacked&threads_per_page=20&posts_per_page=10"},
		{"/admin/users/ban", "uid=2&days=1&reason=x"},
		{"/admin/users/delete", "uid=2"},
		{"/admin/users/group", "uid=2&group=1"},
		{"/admin/forums/save", "id=0&category_id=1&name=注入版块"},
		{"/admin/forums/delete", "id=1"},
		{"/admin/censor/add", "word=注入词"},
		{"/admin/announcements/add", "content=注入公告"},
	}
	for _, op := range adminOnlyOps {
		// 匿名：必须被拒绝（302 跳登录，绝不允许 303 成功跳转）
		w := smokePost(t, op.path, anonCSRF, op.body, anonCookie)
		if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
			t.Errorf("匿名 POST %s → %d %s，应跳登录拒绝", op.path, w.Code, w.Header().Get("Location"))
		}
		// 普通用户：必须 403
		w = smokePost(t, op.path, userCSRF, op.body, userCookie)
		if w.Code != http.StatusForbidden {
			t.Errorf("普通用户 POST %s → %d，应 403", op.path, w.Code)
		}
	}

	// 内容治理操作：普通用户同样 403（版主管辖范围之外亦被 scope 校验拦截）
	staffOps := []struct{ path, body string }{
		{"/admin/threads/action", "op=delete&tid=1"},
		{"/admin/recyclebin/purgeall", ""},
		{"/admin/prune/execute", "kind=post&author=不存在"},
		{"/admin/moderate/thread", "tid=1&op=approve"},
	}
	for _, op := range staffOps {
		w := smokePost(t, op.path, userCSRF, op.body, userCookie)
		if w.Code != http.StatusForbidden {
			t.Errorf("普通用户 POST %s → %d，应 403", op.path, w.Code)
		}
	}

	// 管理员正常可用（禁言再解禁，恢复现场）
	w = smokePost(t, "/admin/users/ban", adminCSRF, "uid=2&days=1&reason=回归", adminCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("管理员禁言 → %d，期望 303", w.Code)
	}
	if w = smokePost(t, "/admin/users/unban", adminCSRF, "uid=2", adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("管理员解禁 → %d，期望 303", w.Code)
	}
}

// TestForumStatsDisplay 首页版块计数与真实内容一致（列序错位回归防线）。
// 冒烟种子：版块 1 含 1 主题（2 楼层），其余版块为空。
func TestForumStatsDisplay(t *testing.T) {
	body := smokeGet(t, "/", nil).Body.String()
	if !strings.Contains(body, "1 / 2") {
		t.Fatal("首页未显示版块 1 的正确计数 1 / 2（疑似 forumCols 列序错位）")
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

// TestAPIJSON 健康检查端点：ok、schema 迁移版本与队列积压结构。
func TestAPIJSON(t *testing.T) {
	w := smokeGet(t, "/api/status", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("/api/status 异常: %d %s", w.Code, body)
	}
	if !strings.Contains(body, `"schema":`) {
		t.Fatal("健康检查缺 schema 迁移版本")
	}
	if !strings.Contains(body, `"pending":`) || !strings.Contains(body, `"reports":`) {
		t.Fatal("健康检查缺队列积压字段")
	}
}

// TestMustChangePasswordGate 阶段五回归：-seed 账号未改密时，
// 内容写入口与管理后台被拦，资料设置页出现强制提示。
func TestMustChangePasswordGate(t *testing.T) {
	ctx := context.Background()
	if _, err := smokePool.Exec(ctx,
		`UPDATE users SET must_change_password=true WHERE id=$1`, 2); err != nil {
		t.Fatal(err)
	}
	defer smokePool.Exec(ctx, `UPDATE users SET must_change_password=false WHERE id=$1`, 2)

	// 发帖表单 GET 403
	w := smokeGet(t, "/new?fid=1", userCookie)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "初始密码") {
		t.Fatalf("未改密发帖表单应 403: %d %s", w.Code, firstLine(w.Body.String()))
	}
	// 回复提交 403
	w = smokePost(t, "/reply/1", userCSRF, "content=x", userCookie)
	if w.Code != http.StatusForbidden {
		t.Fatalf("未改密回复提交应 403: %d", w.Code)
	}

	// 管理员被标记时后台拒绝（改密前锁死后台）
	if _, err := smokePool.Exec(ctx,
		`UPDATE users SET must_change_password=true WHERE id=$1`, 1); err != nil {
		t.Fatal(err)
	}
	w = smokeGet(t, "/admin", adminCookie)
	if w.Code != http.StatusForbidden {
		t.Fatalf("未改密管理员访问后台应 403: %d", w.Code)
	}
	// 资料设置页仍可访问且出现强制提示
	w = smokeGet(t, "/profile", adminCookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "首次登录") {
		t.Fatalf("资料设置应可达且含强制提示: %d", w.Code)
	}
	// 管理员改密后（旧密码验证通过）门禁解除 —— 恢复标志即可，密码不变
	if _, err := smokePool.Exec(ctx,
		`UPDATE users SET must_change_password=false WHERE id=$1`, 1); err != nil {
		t.Fatal(err)
	}
	if w = smokeGet(t, "/admin", adminCookie); w.Code != http.StatusOK {
		t.Fatalf("清除标志后后台应恢复: %d", w.Code)
	}
}

// TestProfileFlow 阶段一回归：资料保存生效、回复历史可见、改密校验旧密码。
// 结束时把 user01 密码改回原值，保持种子状态可复跑。
func TestProfileFlow(t *testing.T) {
	// 资料保存：签名 + 邮箱（保持原邮箱）
	w := smokePost(t, "/profile/save", userCSRF,
		"signature=冒烟签名&email=user%40test.local", userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("保存资料 → %d，期望 303: %s", w.Code, firstLine(w.Body.String()))
	}
	body := smokeGet(t, "/user/2", nil).Body.String()
	if !strings.Contains(body, "冒烟签名") {
		t.Fatal("个人空间未显示新签名")
	}
	if !strings.Contains(body, "回复过的主题") {
		t.Fatal("个人空间缺少回复历史区块")
	}

	// 改密：旧密码错误被拒
	w = smokePost(t, "/profile/password", userCSRF,
		"old_password=wrongpass&new_password=user123456x&confirm_password=user123456x", userCookie)
	if !strings.Contains(w.Body.String(), "当前密码不正确") {
		t.Fatalf("错误旧密码应被拒: %d %s", w.Code, firstLine(w.Body.String()))
	}
	// 改密：两次输入不一致被拒
	w = smokePost(t, "/profile/password", userCSRF,
		"old_password=user123456&new_password=user123456x&confirm_password=other12345", userCookie)
	if !strings.Contains(w.Body.String(), "不一致") {
		t.Fatalf("不一致确认应被拒: %d %s", w.Code, firstLine(w.Body.String()))
	}
	// 改密成功（当前会话保留），再改回
	w = smokePost(t, "/profile/password", userCSRF,
		"old_password=user123456&new_password=user123456x&confirm_password=user123456x", userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("改密 → %d，期望 303: %s", w.Code, firstLine(w.Body.String()))
	}
	w = smokePost(t, "/profile/password", userCSRF,
		"old_password=user123456x&new_password=user123456&confirm_password=user123456", userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("改回密码 → %d，期望 303: %s", w.Code, firstLine(w.Body.String()))
	}
}

// TestReportFlow 阶段三回归：举报提交 → 队列可见 → 驳回后消失；引用预填；匿名拒绝。
func TestReportFlow(t *testing.T) {
	// 匿名举报 → 302 跳登录
	w := smokePost(t, "/report/2", "", "reason=x", nil)
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Fatalf("匿名举报应跳登录: %d %s", w.Code, w.Header().Get("Location"))
	}
	// user01 举报 2 楼（admin 的回复）
	w = smokePost(t, "/report/2", userCSRF, "reason=测试举报理由", userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("举报提交 → %d，期望 303: %s", w.Code, firstLine(w.Body.String()))
	}
	// 审核队列可见（含理由与楼层号）
	body := smokeGet(t, "/admin/moderate", adminCookie).Body.String()
	if !strings.Contains(body, "测试举报理由") || !strings.Contains(body, "待处理举报") {
		t.Fatal("审核队列未显示举报")
	}
	// 驳回后消失
	w = smokePost(t, "/admin/report/handle", adminCSRF, "id=1&op=dismiss", adminCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("驳回 → %d，期望 303: %s", w.Code, firstLine(w.Body.String()))
	}
	body = smokeGet(t, "/admin/moderate", adminCookie).Body.String()
	if strings.Contains(body, "测试举报理由") {
		t.Fatal("驳回后举报仍显示")
	}
	// 引用预填：/reply/1?quote=2 应含引用块
	body = smokeGet(t, "/reply/1?quote=2", userCookie).Body.String()
	if !strings.Contains(body, "引用 admin") {
		t.Fatal("引用预填缺失")
	}
}

// TestRegisterGate 阶段四回归：验证码开关、保留用户名。
// 结束时恢复设置（captcha_enabled=0），保持种子状态可复跑。
func TestRegisterGate(t *testing.T) {
	settingsOn := "site_name=GoBBS 冒烟站&threads_per_page=20&posts_per_page=10" +
		"&register_enabled=1&upload_enabled=1&max_image_mb=8&max_file_mb=20" +
		"&captcha_enabled=1&email_verify_enabled=0&require_consent=1&site_closed=0&site_closed_reason="
	if w := smokePost(t, "/admin/settings", adminCSRF, settingsOn, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("开启验证码设置 → %d", w.Code)
	}
	defer func() {
		settingsOff := strings.Replace(settingsOn, "captcha_enabled=1", "captcha_enabled=0", 1)
		if w := smokePost(t, "/admin/settings", adminCSRF, settingsOff, adminCookie); w.Code != http.StatusSeeOther {
			t.Fatalf("恢复设置 → %d", w.Code)
		}
	}()

	// 注册页出现验证码控件与条款勾选
	body := smokeGet(t, "/register", nil).Body.String()
	if !strings.Contains(body, "captcha_id") || !strings.Contains(body, "/captcha/") {
		t.Fatal("开启后注册页缺验证码控件")
	}
	if !strings.Contains(body, "consent") {
		t.Fatal("注册页缺条款勾选")
	}

	// 匿名会话（自取匿名 CSRF Cookie）
	w := smokeGet(t, "/register", nil)
	var anonCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "forum_csrf" {
			anonCookie = c
		}
	}
	anonCSRF := ""
	if anonCookie != nil {
		anonCSRF = anonCookie.Value
	}

	// 未勾选条款被拒
	w = smokePost(t, "/register", anonCSRF,
		"username=consentuser1&email=&password=pass123456&captcha_id=invalid&captcha=1", anonCookie)
	if !strings.Contains(w.Body.String(), "同意服务条款") {
		t.Fatalf("未勾选条款应被拒: %s", firstLine(w.Body.String()))
	}
	// 错误验证码被拒
	w = smokePost(t, "/register", anonCSRF,
		"username=spamuser01&email=&password=pass123456&consent=1&captcha_id=invalid&captcha=99", anonCookie)
	if !strings.Contains(w.Body.String(), "验证码不正确") {
		t.Fatalf("错误验证码应被拒: %s", firstLine(w.Body.String()))
	}
	// 保留用户名（前缀命中）被拒
	w = smokePost(t, "/register", anonCSRF,
		"username=administrator2&email=&password=pass123456&consent=1&captcha_id=invalid&captcha=1", anonCookie)
	if !strings.Contains(w.Body.String(), "系统保留") {
		t.Fatalf("保留用户名应被拒: %s", firstLine(w.Body.String()))
	}
}

// TestProfileExport 阶段六回归：本人数据导出为 JSON 附件且不含密码哈希，匿名 302。
func TestProfileExport(t *testing.T) {
	w := smokeGet(t, "/profile/export", nil)
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Fatalf("匿名导出应跳登录: %d", w.Code)
	}
	w = smokeGet(t, "/profile/export", userCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("导出 → %d，期望 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("导出类型异常: %s", ct)
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("导出应为附件下载")
	}
	body := w.Body.String()
	if strings.Contains(body, "password_hash") {
		t.Fatal("导出内容不得包含密码哈希")
	}
	// MarshalIndent 输出键后带空格，这里只断言值与字段名存在
	if !strings.Contains(body, "user01") || !strings.Contains(body, "content_md") {
		t.Fatal("导出内容缺账号或楼层字段")
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

// TestModeratorScope 版主管辖口径回归（阶段七）：
// 管辖版块内可见他人楼层删除按钮且删除成功；管辖外按钮不可见且提交 403。
func TestModeratorScope(t *testing.T) {
	// 管辖内：mod01 能看到 admin 回复（post 2）的删除按钮
	body := smokeGet(t, "/thread-1-1-1.html", modCookie).Body.String()
	if !strings.Contains(body, `action="/delete/2"`) {
		t.Fatal("版主在管辖版块应看到他人楼层删除按钮")
	}
	// 管辖外：看不到版块二主题首楼的删除按钮
	body = smokeGet(t, "/thread-"+strconv.FormatInt(smokeTid2, 10)+"-1-1.html", modCookie).Body.String()
	if strings.Contains(body, `action="/delete/`+strconv.FormatInt(smokePid2, 10)+`"`) {
		t.Fatal("版主在管辖外不应看到删除按钮")
	}
	// 管辖外直接提交删除 → 403
	w := smokePost(t, "/delete/"+strconv.FormatInt(smokePid2, 10), modCSRF, "back=/", modCookie)
	if w.Code != http.StatusForbidden {
		t.Fatalf("管辖外删除应 403: %d", w.Code)
	}
	// 管理员不受管辖限制
	body = smokeGet(t, "/thread-"+strconv.FormatInt(smokeTid2, 10)+"-1-1.html", adminCookie).Body.String()
	if !strings.Contains(body, `action="/delete/`+strconv.FormatInt(smokePid2, 10)+`"`) {
		t.Fatal("管理员应看到删除按钮")
	}
	// 普通用户看不到他人楼层删除按钮
	body = smokeGet(t, "/thread-1-1-1.html", userCookie).Body.String()
	own := strings.SplitN(strings.SplitN(body, `id="post1"`, 2)[1], `id="post2"`, 2)[0]
	if strings.Contains(own, `action="/delete/2"`) {
		t.Fatal("普通用户不应看到他人楼层删除按钮")
	}
}

// TestReplyNotification 回复通知楼主回归：admin 回复 user01 的主题 → user01 通知页出现「回复了你的主题」。
func TestReplyNotification(t *testing.T) {
	w := smokePost(t, "/reply/1", adminCSRF, "content=回复通知回归测试", adminCookie)
	if w.Code != http.StatusFound && w.Code != http.StatusSeeOther {
		t.Fatalf("回复 → %d，期望 302/303，Location=%q，body=%s",
			w.Code, w.Header().Get("Location"), firstLine(w.Body.String()))
	}
	body := smokeGet(t, "/notify", userCookie).Body.String()
	if !strings.Contains(body, "回复了你的主题") || !strings.Contains(body, "回复通知回归测试") {
		t.Fatal("楼主未收到回复通知")
	}
}

// TestSEO 阶段七回归：sitemap/rss/robots/meta description/OG。
func TestSEO(t *testing.T) {
	w := smokeGet(t, "/sitemap.xml", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<urlset") || !strings.Contains(w.Body.String(), "thread-1-1-1.html") {
		t.Fatalf("sitemap 异常: %d", w.Code)
	}
	w = smokeGet(t, "/rss", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<rss") || !strings.Contains(w.Body.String(), "冒烟测试主题") {
		t.Fatalf("rss 异常: %d", w.Code)
	}
	w = smokeGet(t, "/robots.txt", nil)
	if !strings.Contains(w.Body.String(), "Sitemap: ") {
		t.Fatal("robots 缺 Sitemap 行")
	}
	body := smokeGet(t, "/thread-1-1-1.html", nil).Body.String()
	if !strings.Contains(body, `name="description"`) || !strings.Contains(body, `property="og:title"`) {
		t.Fatal("帖子页缺 meta description / OG")
	}
	if !strings.Contains(body, "首楼内容") {
		t.Fatal("帖子页 description 未取首楼摘要")
	}
	body = smokeGet(t, "/forum-1-1.html", nil).Body.String()
	if !strings.Contains(body, `name="description"`) || !strings.Contains(body, "冒烟测试版块") {
		t.Fatal("版块页 description 未取版块简介")
	}
}

// TestMoveThread 移帖回归：后台批量移动主题到另一版块，计数与列表同步。
func TestMoveThread(t *testing.T) {
	ctx := context.Background()
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, 2, "user01", "待移动主题", "内容", "<p>内容</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	w := smokePost(t, "/admin/threads/action", adminCSRF,
		"op=move&move_to="+strconv.FormatInt(smokeFid2, 10)+"&tid="+strconv.FormatInt(th.ID, 10), adminCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("移动 → %d，期望 303: %s", w.Code, firstLine(w.Body.String()))
	}
	// 主题已换版块
	body := smokeGet(t, "/forum-"+strconv.FormatInt(smokeFid2, 10)+"-1.html", nil).Body.String()
	if !strings.Contains(body, "待移动主题") {
		t.Fatal("目标版块未出现被移动主题")
	}
	body = smokeGet(t, "/forum-1-1.html", nil).Body.String()
	if strings.Contains(body, "待移动主题") {
		t.Fatal("原版块仍显示被移动主题")
	}
	// 版主管辖口径：目标版块无版主（scope={0}），mod01 不能把主题移入
	w = smokePost(t, "/admin/threads/action", modCSRF,
		"op=move&move_to="+strconv.FormatInt(smokeFid2, 10)+"&tid=1", modCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("移动（越权）→ %d", w.Code)
	}
	if err := smokePool.QueryRow(ctx,
		`SELECT forum_id FROM threads WHERE id=$1`, th.ID).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	var fid int64
	if err := smokePool.QueryRow(ctx,
		`SELECT forum_id FROM threads WHERE id=1`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if fid != 1 {
		t.Fatalf("越权移动不应生效: thread1 forum=%d", fid)
	}
}

// smokeMultipart multipart 表单提交（上传/头像用）。
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

// TestPermMatrix 矩阵入库与生效：关闭会员「编辑自己的楼层」→ user01 编辑 403 → 恢复。
func TestPermMatrix(t *testing.T) {
	w := smokeGet(t, "/admin/perms", adminCookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "权限矩阵") {
		t.Fatalf("矩阵页异常: %d", w.Code)
	}
	w = smokePost(t, "/admin/perms/save", adminCSRF,
		"allow.2.content.moderate=1&allow.2.content.delete.any=1&allow.2.recycle.bin=1&allow.2.prune.run=1&allow.2.moderate.queue=1&allow.2.upload.use=1"+
			"&allow.0.content.delete.own=1&allow.0.upload.use=1", adminCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("保存矩阵 → %d", w.Code)
	}
	w = smokePost(t, "/edit/1", userCSRF, "subject=冒烟测试主题&content=x&version=1", userCookie)
	if w.Code != http.StatusForbidden {
		t.Fatalf("关闭后编辑应 403: %d", w.Code)
	}
	w = smokeGet(t, "/admin", adminCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("admin.panel 硬保护被破坏: %d", w.Code)
	}
	body := "allow.0.content.edit.own=1&allow.0.content.delete.own=1&allow.0.upload.use=1" +
		"&allow.2.content.moderate=1&allow.2.content.delete.any=1&allow.2.recycle.bin=1&allow.2.prune.run=1&allow.2.moderate.queue=1&allow.2.upload.use=1"
	w = smokePost(t, "/admin/perms/save", adminCSRF, body, adminCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("恢复矩阵 → %d", w.Code)
	}
	w = smokePost(t, "/edit/1", userCSRF, "subject=冒烟测试主题&content=x&version=1", userCookie)
	if w.Code == http.StatusForbidden {
		t.Fatal("恢复后编辑仍 403")
	}
}

// TestBlockUser 封禁（禁止登录）：会话立即失效、登录被拒、解封恢复。
func TestBlockUser(t *testing.T) {
	ctx := context.Background()
	w := smokePost(t, "/admin/users/block", adminCSRF, "uid=2&days=1", adminCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("封禁 → %d", w.Code)
	}
	w = smokeGet(t, "/notify", userCookie)
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Fatalf("封禁后旧会话应失效: %d", w.Code)
	}
	// 匿名 CSRF 取一次并复用（两次取样是不同 token，会 403）
	c := anonLoginCookie(t)
	w = smokePost(t, "/login", c.Value, "username=user01&password=user123456", c)
	if !strings.Contains(w.Body.String(), "账号已被封禁") {
		t.Fatalf("封禁账号登录应被拒: %d %s", w.Code, firstLine(w.Body.String()))
	}
	if w = smokePost(t, "/admin/users/unblock", adminCSRF, "uid=2", adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("解封 → %d", w.Code)
	}
	// 封禁时 user01 全部会话已被删除：换发新会话，恢复后续测试依赖
	tokU, csrfU, err := smokeSrv.st.CreateSession(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	userCookie = &http.Cookie{Name: "forum_session", Value: tokU}
	userCSRF = csrfU
}

// anonLoginCookie / anonLoginCSRF 取匿名 CSRF 会话（登录表单用）。
func anonLoginCookie(t *testing.T) *http.Cookie {
	t.Helper()
	w := smokeGet(t, "/login", nil)
	for _, c := range w.Result().Cookies() {
		if c.Name == "forum_csrf" {
			return c
		}
	}
	return nil
}

func anonLoginCSRF(t *testing.T) string {
	t.Helper()
	c := anonLoginCookie(t)
	if c == nil {
		return ""
	}
	return c.Value
}

// TestAvatarUpload 头像上传：/avatar/{uid} 优先返回自定义图片；清除后回到 SVG。
func TestAvatarUpload(t *testing.T) {
	w := smokeMultipart(t, "/profile/avatar", userCSRF, "avatar", "a.png", pngMagic, nil, userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("上传头像 → %d，Location=%q，body=%s", w.Code, w.Header().Get("Location"), firstLine(w.Body.String()))
	}
	w = smokeGet(t, "/avatar/2", nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "image/png") {
		t.Fatalf("自定义头像应生效: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	w = smokePost(t, "/profile/avatar/clear", userCSRF, "", userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("清除头像 → %d", w.Code)
	}
	w = smokeGet(t, "/avatar/2", nil)
	if !strings.Contains(w.Header().Get("Content-Type"), "image/svg") {
		t.Fatal("清除后应回到 SVG 头像")
	}
}

// TestAttachments 附件挂楼层：上传→回复引用→楼层出现附件区。
func TestAttachments(t *testing.T) {
	w := smokeMultipart(t, "/api/upload", userCSRF, "file", "report.pdf", pdfMagic,
		map[string]string{"kind": "file"}, userCookie)
	if w.Code != 200 {
		t.Fatalf("上传附件 → %d %s", w.Code, w.Body.String())
	}
	var resp struct{ URL string }
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.URL == "" {
		t.Fatal("上传未返回 URL")
	}
	w = smokePost(t, "/reply/1", userCSRF, "content=附件见 [文件]("+resp.URL+")", userCookie)
	if w.Code != http.StatusFound && w.Code != http.StatusSeeOther {
		t.Fatalf("回复 → %d", w.Code)
	}
	body := smokeGet(t, "/thread-1-1-1.html", nil).Body.String()
	if !strings.Contains(body, "attachments") || !strings.Contains(body, "report.pdf") {
		t.Fatal("楼层未展示附件区")
	}
}

var pdfMagic = []byte("%PDF-1.4\n%test\n")

// TestSelfDelete 自助删号：无内容用户密码确认后删号；有内容用户被拒。
func TestSelfDelete(t *testing.T) {
	ctx := context.Background()
	u, err := smokeSrv.st.CreateUser(ctx, "selfdel01", "selfdel123", "")
	if err != nil {
		t.Fatal(err)
	}
	tok, csrf, err := smokeSrv.st.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Cookie{Name: "forum_session", Value: tok}
	w := smokePost(t, "/profile/delete", csrf, "password=selfdel123", c)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("自助删号 → %d", w.Code)
	}
	var n int
	if err := smokePool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, u.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("用户应已删除")
	}
	w = smokePost(t, "/profile/delete", userCSRF, "password=user123456", userCookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("有内容用户删号 → %d", w.Code)
	}
	if err := smokePool.QueryRow(ctx, `SELECT count(*) FROM users WHERE username='user01'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("有内容用户不应被删除")
	}
}

// TestSetupRedirect 已有用户时 /setup 重定向回首页（不重复安装）。
func TestSetupRedirect(t *testing.T) {
	w := smokeGet(t, "/setup", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("已有用户时 /setup 应重定向: %d", w.Code)
	}
}
