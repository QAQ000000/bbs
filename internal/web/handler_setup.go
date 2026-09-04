// SPDX-License-Identifier: AGPL-3.0-or-later
// handler_setup.go：安装向导与权限矩阵页（ROADMAP P1-6 / P1-14）。
package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dzforum/internal/perm"
)

// ---- 权限矩阵（P1-6）----

var pointLabels = map[perm.Point]string{
	perm.AdminPanel:       "进入后台",
	perm.ForumManage:      "版块与分类管理",
	perm.ContentModerate:  "内容治理入口",
	perm.ContentEditOwn:   "编辑自己的楼层",
	perm.ContentEditAny:   "编辑任何人的楼层",
	perm.ContentDeleteOwn: "删除自己的楼层",
	perm.ContentDeleteAny: "删除任何人的楼层",
	perm.UserBan:          "禁言 / 封禁",
	perm.UserDelete:       "删号",
	perm.UserSetGroup:     "调整用户组",
	perm.SettingsEdit:     "站点设置",
	perm.CensorManage:     "敏感词管理",
	perm.AnnounceManage:   "公告管理",
	perm.LogsView:         "查看管理日志",
	perm.RecycleBin:       "回收站管理",
	perm.PruneRun:         "批量删帖",
	perm.ModerateQueue:    "审核队列",
	perm.UploadUse:        "使用本站上传",
}

func (s *Server) adminPerms(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	data := struct {
		Common
		Points      []perm.Point
		Roles       []perm.Role
		RoleNames   map[perm.Role]string
		PointLabels map[perm.Point]string
		Matrix      map[perm.Role]map[perm.Point]bool
	}{s.common(r), perm.AllPoints(),
		[]perm.Role{perm.RoleMember, perm.RoleModerator, perm.RoleAdmin},
		map[perm.Role]string{perm.RoleMember: "会员", perm.RoleModerator: "版主", perm.RoleAdmin: "管理员"},
		pointLabels, perm.Matrix()}
	data.Title = "权限矩阵"
	_ = s.rd.Render(w, "admin_perms.html", &data)
}

func (s *Server) adminPermsSave(w http.ResponseWriter, r *http.Request) {
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
			if role == perm.RoleAdmin && pt == perm.AdminPanel {
				m[role][pt] = true
				continue
			}
			key := "allow." + strconv.Itoa(int(role)) + "." + string(pt)
			m[role][pt] = r.PostFormValue(key) == "1"
		}
	}
	if err := s.st.SaveRolePerms(r.Context(), m); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	s.logOp(r, "perms.save", "更新权限矩阵")
	s.setFlash(w, "权限矩阵已保存并即时生效")
	http.Redirect(w, r, "/admin/perms", http.StatusSeeOther)
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

// setupGuard 安装向导闸门：空库时前台一律跳 /setup。
func (s *Server) setupGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.setupRequired() {
			p := r.URL.Path
			if p != "/setup" && !strings.HasPrefix(p, "/static/") && p != "/favicon.ico" {
				http.Redirect(w, r, "/setup", http.StatusFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setupForm(w http.ResponseWriter, r *http.Request) {
	if !s.setupRequired() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	d := struct {
		Common
		CSRF     string
		Error    string
		SiteName string
		Username string
		Email    string
	}{s.common(r), s.anonCSRF(r, w), "", s.cfg.SiteName, "", ""}
	d.Title = "初始化站点"
	_ = s.rd.Render(w, "page_setup.html", &d)
}

func (s *Server) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.setupRequired() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if !s.allow(r, "setup", 10, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	siteName := strings.TrimSpace(r.PostFormValue("site_name"))
	username := strings.TrimSpace(r.PostFormValue("username"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm_password")

	fail := func(msg string) {
		d := struct {
			Common
			CSRF     string
			Error    string
			SiteName string
			Username string
			Email    string
		}{s.common(r), s.anonCSRF(r, w), msg, siteName, username, email}
		d.Title = "初始化站点"
		_ = s.rd.Render(w, "page_setup.html", &d)
	}
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
	u, err := s.st.CreateUser(r.Context(), username, password, email)
	if err != nil {
		if strings.Contains(err.Error(), "users_username_lower_idx") {
			fail("用户名已被占用")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "安装失败", err.Error())
		return
	}
	if err := s.st.PromoteToAdmin(r.Context(), u.ID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "安装失败", err.Error())
		return
	}
	_ = s.st.SaveSettings(r.Context(), map[string]string{"site_name": siteName})
	s.logOp(r, "setup", "站点初始化：管理员 "+username+"，站点名 "+siteName)

	token, _, err := s.st.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "安装失败", err.Error())
		return
	}
	s.setSessionCookie(w, token, s.cfg.CookieTTL)
	s.setFlash(w, "安装完成，欢迎！")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setupMu / setupCheckedAt / setupRequiredFlag 由 Server 持有（见 server.go）。
var _ sync.Mutex
