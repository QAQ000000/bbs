// SPDX-License-Identifier: AGPL-3.0-or-later

// zz_security_smoke_test.go：安全整改批次 HTTP 层回归。
// 文件名 zz_ 前缀保证在基线冒烟测试之后运行（会改动种子数据）。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"dzforum/internal/perm"

	"strconv"
	"strings"
	"testing"
)

// fullMatrixBody 全角色默认矩阵（后台保存按「缺失即 false」全量写入，
// 恢复基线必须携带全部角色的全部权限点，否则管理员细粒度点会被清空）。
func fullMatrixBody() string {
	body := url.Values{}
	for role, points := range perm.Defaults() {
		for _, point := range perm.AllPoints() {
			value := "0"
			if points[point] {
				value = "1"
			}
			body.Set("allow."+strconv.Itoa(int(role))+"."+string(point), value)
		}
	}
	return body.Encode()
}

// TestDigestSortPage 精华筛选 200 回归（count 查询缺 t 别名曾 500）。
func TestPermPointGuards(t *testing.T) {
	requireDB(t)
	// 关闭 role2 的 moderate.queue（保留其余）
	body := "allow.0.content.edit.own=1&allow.0.content.delete.own=1&allow.0.upload.use=1" +
		"&allow.2.content.moderate=1&allow.2.content.delete.any=1&allow.2.recycle.bin=1&allow.2.prune.run=1&allow.2.upload.use=1"
	if w := smokePost(t, "/api/v1/admin/perms/save", adminCSRF, body, adminCookie); w.Code != http.StatusOK {
		t.Fatalf("保存矩阵 → %d", w.Code)
	}
	if w := smokeGet(t, "/api/v1/admin/moderate", modCookie); w.Code != http.StatusForbidden {
		t.Fatalf("moderate.queue 关闭后审核页应 403: %d", w.Code)
	}
	if w := smokeGet(t, "/api/v1/admin/recyclebin", modCookie); w.Code != http.StatusOK {
		t.Fatalf("recycle.bin 未关闭应仍可访问: %d", w.Code)
	}
	for _, path := range []string{"/api/v1/admin/membership", "/api/v1/admin/users"} {
		if w := smokeGet(t, path, adminCookie); w.Code != http.StatusForbidden {
			t.Fatalf("omitted permissions must be disabled: %s returned %d", path, w.Code)
		}
	}
	// 恢复全角色默认矩阵
	if w := smokePost(t, "/api/v1/admin/perms/save", adminCSRF, fullMatrixBody(), adminCookie); w.Code != http.StatusOK {
		t.Fatalf("恢复矩阵 → %d", w.Code)
	}
	if w := smokeGet(t, "/api/v1/admin/moderate", modCookie); w.Code != http.StatusOK {
		t.Fatalf("恢复后审核页应 200: %d", w.Code)
	}
}

// TestBanPermanentHTTP 永久禁言（days=0）后用户不能发帖、后台用户列表正常。
func TestBanPermanentHTTP(t *testing.T) {
	requireDB(t)
	if w := smokePost(t, "/api/v1/admin/users/ban", adminCSRF, "uid=3&days=0&reason=回归测试", adminCookie); w.Code != http.StatusOK {
		t.Fatalf("永久禁言 → %d", w.Code)
	}
	if w := smokePost(t, "/api/v1/threads/1/posts", modCSRF, "content=禁言期发言", modCookie); w.Code != http.StatusForbidden {
		t.Fatalf("禁言用户发帖应 403: %d", w.Code)
	}
	if w := smokeGet(t, "/api/v1/admin/users", adminCookie); w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("用户列表应 200（永久禁言扫描曾失败）: %d", w.Code)
	}
	if w := smokePost(t, "/api/v1/admin/users/unban", adminCSRF, "uid=3", adminCookie); w.Code != http.StatusOK {
		t.Fatalf("解禁 → %d", w.Code)
	}
	if w := smokeGet(t, "/api/v1/admin/moderate", modCookie); w.Code != http.StatusOK {
		t.Fatalf("解禁后版主应恢复正常: %d", w.Code)
	}
}

// TestEditConflictHTTP 过期版本提交编辑 → 409（SQL 未带版本条件时后写覆盖先写）。
func TestEditConflictHTTP(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	p, err := smokeSrv.st.Post(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	w := smokePatch(t, "/api/v1/posts/1", userCSRF,
		"subject=冒烟测试主题&content=过期版本提交&version="+strconv.Itoa(p.Version+100), userCookie)
	if w.Code != http.StatusConflict {
		t.Fatalf("过期版本应 409: %d", w.Code)
	}
	// 当前版本提交成功
	w = smokePatch(t, "/api/v1/posts/1", userCSRF,
		"subject=冒烟测试主题&content=当前版本提交&version="+strconv.Itoa(p.Version), userCookie)
	if w.Code == http.StatusConflict {
		t.Fatal("当前版本提交不应 409")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("当前版本编辑 → %d", w.Code)
	}
}

// TestAvatarOversizeRejected 超过 2MB 的头像被拒绝且旧头像保留。
func TestAvatarOversizeRejected(t *testing.T) {
	requireDB(t)
	// 先上传一张正常头像
	if w := smokeMultipart(t, "/api/v1/me/avatar", userCSRF, "avatar", "a.png", pngMagic, nil, userCookie); w.Code != http.StatusOK {
		t.Fatalf("正常头像上传 → %d", w.Code)
	}
	big := make([]byte, 3<<20)
	copy(big, pngMagic)
	w := smokeMultipart(t, "/api/v1/me/avatar", userCSRF, "avatar", "big.png", big, nil, userCookie)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大头像应 413: %d", w.Code)
	}
	// 旧头像未丢
	if w := smokeGet(t, "/avatar/2", nil); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "image/png") {
		t.Fatal("超大上传失败后旧头像应保留")
	}
	// 清理：恢复默认头像，不影响后续
	apiRequest(t, http.MethodDelete, "/api/v1/me/avatar", userCSRF, "{}", userCookie)
}

// TestQuoteTruncation 引用块严格截断（单行长段落也曾整行带入）。
