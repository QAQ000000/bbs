// SPDX-License-Identifier: AGPL-3.0-or-later
// handler_setup.go：安装向导与权限矩阵页（ROADMAP P1-6 / P1-14）。
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// ---- 权限矩阵（P1-6）----

func (s *Server) adminPermsSave(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	m := map[perm.Role]map[perm.Point]bool{}
	for _, role := range []perm.Role{perm.RoleMember, perm.RoleModerator, perm.RoleAdmin} {
		m[role] = map[perm.Point]bool{}
		for _, pt := range perm.AllPoints() {
			// AdminPanel×管理员硬保护：无论如何强制开启，防止自锁
			if role == perm.RoleAdmin && (pt == perm.AdminPanel || pt == perm.PermissionsEdit) {
				m[role][pt] = true
				continue
			}
			key := "allow." + strconv.Itoa(int(role)) + "." + string(pt)
			m[role][pt] = r.PostFormValue(key) == "1"
		}
	}
	if err := s.st.SaveRolePerms(r.Context(), m); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	s.logOp(r, "perms.save", "更新权限矩阵")
	result.Message = "权限矩阵已保存并即时生效"
	s.respond(w, http.StatusOK, result)
}

// ---- 安装向导（P1-14）----

func (s *Server) setupRequired() bool {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if time.Now().Before(s.setupCheckedAt) {
		return s.setupRequiredFlag
	}
	ok, err := s.st.HasUsers(context.Background())
	s.setupRequiredFlag = err == nil && !ok
	s.setupCheckedAt = time.Now().Add(5 * time.Second)
	return s.setupRequiredFlag
}

func (s *Server) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.setupRequired() {
		s.fail(w, r, http.StatusConflict, "ALREADY_INITIALIZED", "站点已初始化")
		return
	}
	if !s.allow(r, "setup", 10, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	siteName := strings.TrimSpace(r.PostFormValue("site_name"))
	username := strings.TrimSpace(r.PostFormValue("username"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm_password")

	fail := func(msg string) { s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg) }
	if siteName == "" || utf8.RuneCountInString(siteName) > 40 {
		fail("站点名称不能为空且不超过 40 字")
		return
	}
	if !usernameRe.MatchString(username) || reservedName(username) {
		fail("用户名需为 2-15 位中文、字母、数字或下划线，且不为保留名")
		return
	}
	if utf8.RuneCountInString(password) < 8 {
		fail("密码至少 8 位")
		return
	}
	if password != confirm {
		fail("两次输入的密码不一致")
		return
	}
	if email != "" && !emailRe.MatchString(email) {
		fail("邮箱格式不正确")
		return
	}
	u, err := s.st.InitializeSite(r.Context(), username, password, email, siteName)
	if errors.Is(err, store.ErrAlreadyInitialized) {
		s.setupMu.Lock()
		s.setupRequiredFlag = false
		s.setupCheckedAt = time.Time{}
		s.setupMu.Unlock()
		s.fail(w, r, http.StatusConflict, "ALREADY_INITIALIZED", "站点已初始化")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "users_username_lower_idx") {
			fail("用户名已被占用")
			return
		}
		s.fail(w, r, http.StatusInternalServerError, "安装失败", err.Error())
		return
	}
	s.setupMu.Lock()
	s.setupRequiredFlag = false
	s.setupCheckedAt = time.Time{}
	s.setupMu.Unlock()
	s.logOp(r, "setup", "站点初始化：管理员 "+username+"，站点名 "+siteName)

	token, _, err := s.st.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "安装失败", err.Error())
		return
	}
	s.setSessionCookie(w, token, s.cfg.CookieTTL)

	s.respond(w, http.StatusOK, privateUser(u))
}

// setupMu / setupCheckedAt / setupRequiredFlag 由 Server 持有（见 server.go）。
