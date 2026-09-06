// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"net/http"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// ---- 后台公共 ----

// requireAdmin 后台访问守卫：需 AdminPanel 权限点（管理员）。
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	u := User(r)
	if u == nil {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "请先登录")
		return false
	}
	if !hasPoint(u, perm.AdminPanel) {
		s.fail(w, r, http.StatusForbidden, "无权访问", "该区域仅管理员可访问。")
		return false
	}
	if u.MustChangePassword {
		s.fail(w, r, http.StatusForbidden, "请先修改初始密码",
			"检测到您仍在使用初始密码，请先在「资料设置」中修改密码，再使用管理后台。")
		return false
	}
	return true
}

func (s *Server) sets(r *http.Request) store.SiteSettings {
	return s.st.Settings(r.Context())
}
