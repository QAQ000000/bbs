// SPDX-License-Identifier: AGPL-3.0-or-later

// zz_security_smoke_test.go：安全整改批次 HTTP 层回归。
// 文件名 zz_ 前缀保证在基线冒烟测试之后运行（会改动种子数据）。

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"dzforum/internal/store"
)

// fullMatrixBody 全角色默认矩阵（后台保存按「缺失即 false」全量写入，
// 恢复基线必须携带全部角色的全部权限点，否则管理员细粒度点会被清空）。
func fullMatrixBody() string {
	return "allow.0.content.edit.own=1&allow.0.content.delete.own=1&allow.0.upload.use=1" +
		"&allow.2.content.moderate=1&allow.2.content.delete.any=1&allow.2.recycle.bin=1" +
		"&allow.2.prune.run=1&allow.2.moderate.queue=1&allow.2.upload.use=1" +
		"&allow.1.admin.panel=1&allow.1.forum.manage=1&allow.1.content.moderate=1&allow.1.content.edit.own=1&allow.1.content.edit.any=1&allow.1.content.delete.own=1&allow.1.content.delete.any=1&allow.1.user.ban=1&allow.1.user.delete=1&allow.1.user.group=1&allow.1.settings.edit=1&allow.1.censor.manage=1&allow.1.announce.manage=1&allow.1.logs.view=1&allow.1.recycle.bin=1&allow.1.prune.run=1&allow.1.moderate.queue=1&allow.1.upload.use=1"
}


// TestDigestSortPage 精华筛选 200 回归（count 查询缺 t 别名曾 500）。
func TestDigestSortPage(t *testing.T) {
	for _, sort := range []string{"", "digest", "new", "hot"} {
		u := "/forum-1-1.html"
		if sort != "" {
			u += "?sort=" + sort
		}
		w := smokeGet(t, u, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "</html>") {
			t.Fatalf("sort=%q → %d，期望 200 完整页", sort, w.Code)
		}
	}
}

// TestQuotePendingHidden 引用入口不泄露待审正文（1）；
// 待审主题的回复页对无关用户不可达。
func TestQuotePendingHidden(t *testing.T) {
	ctx := context.Background()
	// 无关第三人（非作者非版主）
	third, err := smokeSrv.st.CreateUser(ctx, "third01", "third123456", "")
	if err != nil {
		t.Fatal(err)
	}
	tok, csrfT, err := smokeSrv.st.CreateSession(ctx, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	thirdCookie := &http.Cookie{Name: "forum_session", Value: tok}
	_ = csrfT

	// user01 的待审回复
	_, pending, err := smokeSrv.st.CreateReply(ctx, 1, 2, "user01",
		"机密待审正文内容", "<p>机密待审正文内容</p>", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	// 第三人通过 quote 参数读待审楼层：不得预填正文
	w := smokeGet(t, "/reply/1?quote="+strconv.FormatInt(pending.ID, 10), thirdCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("引用待审楼层 → %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "机密待审正文内容") {
		t.Fatal("待审楼层正文不得通过引用入口泄露")
	}
	// 作者本人可以引用自己的待审楼层
	w = smokeGet(t, "/reply/1?quote="+strconv.FormatInt(pending.ID, 10), userCookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "机密待审正文内容") {
		t.Fatal("作者引用自己的待审楼层应预填正文")
	}
	// 公开楼层引用仍可用（回归）
	w = smokeGet(t, "/reply/1?quote=2", thirdCookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "回复内容") {
		t.Fatal("公开楼层引用失效")
	}

	// 待审主题：无关用户回复页 404，作者可见
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, 2, "user01",
		"待审主题引用回归", "待审首楼", "<p>待审首楼</p>", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	w = smokeGet(t, "/reply/"+strconv.FormatInt(th.ID, 10), thirdCookie)
	if w.Code != http.StatusNotFound {
		t.Fatalf("无关用户回复待审主题应 404: %d", w.Code)
	}
	w = smokeGet(t, "/reply/"+strconv.FormatInt(th.ID, 10), userCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("作者回复自己的待审主题应 200: %d", w.Code)
	}
}

// TestAdminModerateRendersPending 有待审回复时审核页 200（postCols 缺 IP 扫描曾 500）。
func TestAdminModerateRendersPending(t *testing.T) {
	ctx := context.Background()
	if _, _, err := smokeSrv.st.CreateReply(ctx, 1, 2, "user01",
		"审核页渲染回归待审回复", "<p>x</p>", true, "manual"); err != nil {
		t.Fatal(err)
	}
	w := smokeGet(t, "/admin/moderate", adminCookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "</html>") {
		t.Fatalf("审核页 → %d（曾因待审楼层扫描缺列 500）", w.Code)
	}
	if !strings.Contains(w.Body.String(), "审核页渲染回归待审回复") {
		t.Fatal("待审回复应出现在审核队列")
	}
}

// TestUploadsAuthorization 上传文件授权：无记录 404、未挂载仅本人、
// 公开楼层附件公开、非公开楼层附件收紧；目录不列。
func TestUploadsAuthorization(t *testing.T) {
	w := smokeMultipart(t, "/api/upload", userCSRF, "file", "authz.png", pngMagic, nil, userCookie)
	if w.Code != 200 {
		t.Fatalf("上传 → %d %s", w.Code, w.Body.String())
	}
	var resp struct{ URL string }
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.URL == "" || !strings.HasPrefix(resp.URL, "/uploads/") {
		t.Fatalf("上传返回异常: %q", resp.URL)
	}
	// 未挂载：游客 404，本人 200
	if w := smokeGet(t, resp.URL, nil); w.Code != http.StatusNotFound {
		t.Fatalf("未挂载文件对游客应 404: %d", w.Code)
	}
	if w := smokeGet(t, resp.URL, userCookie); w.Code != http.StatusOK {
		t.Fatalf("上传者本人应可取: %d", w.Code)
	}
	// 目录与无记录路径一律 404
	for _, p := range []string{"/uploads/", "/uploads/nope.png", "/uploads/../db/schema.sql", "/uploads/9999/01/x.png"} {
		if w := smokeGet(t, p, nil); w.Code == http.StatusOK {
			t.Fatalf("%s 不应对游客开放", p)
		}
	}
	// 挂到公开楼层：游客可取
	w = smokePost(t, "/reply/1", userCSRF, "content=图："+resp.URL, userCookie)
	if w.Code != http.StatusFound && w.Code != http.StatusSeeOther {
		t.Fatalf("回复 → %d", w.Code)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	frag := loc.Fragment
	pid, err := strconv.ParseInt(strings.TrimPrefix(frag, "post"), 10, 64)
	if err != nil {
		t.Fatalf("无法解析新楼层 id: %q", loc.String())
	}
	if w := smokeGet(t, resp.URL, nil); w.Code != http.StatusOK {
		t.Fatalf("公开楼层附件游客应可取: %d", w.Code)
	}
	// 楼层转入待审：游客 404，本人与管理员仍可取
	if err := smokeSrv.st.SetPostPendingModeration(context.Background(), pid, "manual"); err != nil {
		t.Fatal(err)
	}
	if w := smokeGet(t, resp.URL, nil); w.Code != http.StatusNotFound {
		t.Fatalf("待审楼层附件对游客应 404: %d", w.Code)
	}
	if w := smokeGet(t, resp.URL, userCookie); w.Code != http.StatusOK {
		t.Fatalf("待审楼层附件本人应可取: %d", w.Code)
	}
	if w := smokeGet(t, resp.URL, adminCookie); w.Code != http.StatusOK {
		t.Fatalf("待审楼层附件管理员应可取: %d", w.Code)
	}
}

// TestPermPointGuards 矩阵撤销即时生效：版主有 content.moderate 但
// moderate.queue 关闭 → 审核页 403；恢复后可用。
func TestPermPointGuards(t *testing.T) {
	// 关闭 role2 的 moderate.queue（保留其余）
	body := "allow.0.content.edit.own=1&allow.0.content.delete.own=1&allow.0.upload.use=1" +
		"&allow.2.content.moderate=1&allow.2.content.delete.any=1&allow.2.recycle.bin=1&allow.2.prune.run=1&allow.2.upload.use=1"
	if w := smokePost(t, "/admin/perms/save", adminCSRF, body, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("保存矩阵 → %d", w.Code)
	}
	if w := smokeGet(t, "/admin/moderate", modCookie); w.Code != http.StatusForbidden {
		t.Fatalf("moderate.queue 关闭后审核页应 403: %d", w.Code)
	}
	if w := smokeGet(t, "/admin/recyclebin", modCookie); w.Code != http.StatusOK {
		t.Fatalf("recycle.bin 未关闭应仍可访问: %d", w.Code)
	}
	// 恢复全角色默认矩阵
	if w := smokePost(t, "/admin/perms/save", adminCSRF, fullMatrixBody(), adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("恢复矩阵 → %d", w.Code)
	}
	if w := smokeGet(t, "/admin/moderate", modCookie); w.Code != http.StatusOK {
		t.Fatalf("恢复后审核页应 200: %d", w.Code)
	}
}

// TestBanPermanentHTTP 永久禁言（days=0）后用户不能发帖、后台用户列表正常。
func TestBanPermanentHTTP(t *testing.T) {
	if w := smokePost(t, "/admin/users/ban", adminCSRF, "uid=3&days=0&reason=回归测试", adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("永久禁言 → %d", w.Code)
	}
	if w := smokePost(t, "/reply/1", modCSRF, "content=禁言期发言", modCookie); w.Code != http.StatusForbidden {
		t.Fatalf("禁言用户发帖应 403: %d", w.Code)
	}
	if w := smokeGet(t, "/admin/users", adminCookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "</html>") {
		t.Fatalf("用户列表应 200（永久禁言扫描曾失败）: %d", w.Code)
	}
	if w := smokePost(t, "/admin/users/unban", adminCSRF, "uid=3", adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("解禁 → %d", w.Code)
	}
	if w := smokeGet(t, "/admin/moderate", modCookie); w.Code != http.StatusOK {
		t.Fatalf("解禁后版主应恢复正常: %d", w.Code)
	}
}

// TestEditConflictHTTP 过期版本提交编辑 → 409（SQL 未带版本条件时后写覆盖先写）。
func TestEditConflictHTTP(t *testing.T) {
	ctx := context.Background()
	p, err := smokeSrv.st.Post(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	w := smokePost(t, "/edit/1", userCSRF,
		"subject=冒烟测试主题&content=过期版本提交&version="+strconv.Itoa(p.Version+100), userCookie)
	if w.Code != http.StatusConflict {
		t.Fatalf("过期版本应 409: %d", w.Code)
	}
	// 当前版本提交成功
	w = smokePost(t, "/edit/1", userCSRF,
		"subject=冒烟测试主题&content=当前版本提交&version="+strconv.Itoa(p.Version), userCookie)
	if w.Code == http.StatusConflict {
		t.Fatal("当前版本提交不应 409")
	}
	if w.Code != http.StatusFound && w.Code != http.StatusSeeOther {
		t.Fatalf("当前版本编辑 → %d", w.Code)
	}
}

// TestAvatarOversizeRejected 超过 2MB 的头像被拒绝且旧头像保留。
func TestAvatarOversizeRejected(t *testing.T) {
	// 先上传一张正常头像
	if w := smokeMultipart(t, "/profile/avatar", userCSRF, "avatar", "a.png", pngMagic, nil, userCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("正常头像上传 → %d", w.Code)
	}
	big := make([]byte, 3<<20)
	copy(big, pngMagic)
	w := smokeMultipart(t, "/profile/avatar", userCSRF, "avatar", "big.png", big, nil, userCookie)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大头像应 413: %d", w.Code)
	}
	// 旧头像未丢
	if w := smokeGet(t, "/avatar/2", nil); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "image/png") {
		t.Fatal("超大上传失败后旧头像应保留")
	}
	// 清理：恢复默认头像，不影响后续
	smokePost(t, "/profile/avatar/clear", userCSRF, "", userCookie)
}

// TestQuoteTruncation 引用块严格截断（单行长段落也曾整行带入）。
func TestQuoteTruncation(t *testing.T) {
	long := strings.Repeat("长", 300)
	q := buildQuote(&store.Post{AuthorName: "u", Floor: 2, ContentMD: long})
	if !strings.Contains(q, "引用 u（2 楼）") {
		t.Fatal("引用块结构缺失")
	}
	if n := utf8.RuneCountInString(q); n > 200 {
		t.Fatalf("引用块应截断: %d runes", n)
	}
	if !strings.Contains(q, "…") {
		t.Fatal("截断的引用应带省略号")
	}
}
