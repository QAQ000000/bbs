// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"dzforum/internal/store"
)

var usernameRe = regexp.MustCompile(`^[\p{Han}a-zA-Z0-9_]{2,15}$`)
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// safeNext 只允许站内相对地址回跳。
func safeNext(s string) string {
	if s == "" || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") {
		return "/"
	}
	return s
}

func nextOf(r *http.Request) string {
	return safeNext(r.URL.Query().Get("next"))
}

// ---- 登录 ----

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if User(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	d := struct {
		Common
		Next  string
		CSRF  string
		Error string
	}{s.common(r), nextOf(r), s.anonCSRF(r, w), ""}
	_ = s.rd.Render(w, "page_login.html", &d)
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	var fail = func(msg string) {
		d := struct {
			Common
			Next  string
			CSRF  string
			Error string
		}{s.common(r), next, s.anonCSRF(r, w), msg}
		w.WriteHeader(http.StatusOK)
		_ = s.rd.Render(w, "page_login.html", &d)
	}
	if username == "" || password == "" {
		fail("请输入用户名和密码")
		return
	}
	u, err := s.st.UserByName(r.Context(), username)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !s.st.VerifyPassword(u, password)) {
		s.st.AdminLog(r.Context(), 0, username, "login.fail", "登录失败：用户名或密码不正确", remoteIP(r))
		fail("用户名或密码不正确")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "登录失败", err.Error())
		return
	}
	token, _, err := s.st.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "登录失败", err.Error())
		return
	}
	if u.IsAdmin() {
		s.st.AdminLog(r.Context(), u.ID, u.Username, "login.ok", "管理员登录", remoteIP(r))
	}
	s.st.TouchLogin(r.Context(), u.ID)
	s.setSessionCookie(w, token, s.cfg.CookieTTL)
	s.setFlash(w, "欢迎回来，"+u.Username+"！")
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// ---- 注册 ----

func (s *Server) registerForm(w http.ResponseWriter, r *http.Request) {
	if !s.sets(r).RegisterEnabled {
		s.renderError(w, r, http.StatusForbidden, "注册已关闭", "本站已关闭新用户注册，请联系管理员。")
		return
	}
	if User(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	d := struct {
		Common
		Next  string
		CSRF  string
		Error string
	}{s.common(r), nextOf(r), s.anonCSRF(r, w), ""}
	_ = s.rd.Render(w, "page_register.html", &d)
}

func (s *Server) registerSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.sets(r).RegisterEnabled {
		s.renderError(w, r, http.StatusForbidden, "注册已关闭", "本站已关闭新用户注册，请联系管理员。")
		return
	}
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	var fail = func(msg string) {
		d := struct {
			Common
			Next      string
			CSRF      string
			Error     string
			Username  string
			Email     string
		}{s.common(r), next, s.anonCSRF(r, w), msg, username, email}
		_ = s.rd.Render(w, "page_register.html", &d)
	}
	if !usernameRe.MatchString(username) {
		fail("用户名需为 2-15 位中文、字母、数字或下划线")
		return
	}
	if utf8.RuneCountInString(password) < 6 {
		fail("密码至少 6 位")
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
		} else {
			s.renderError(w, r, http.StatusInternalServerError, "注册失败", err.Error())
		}
		return
	}
	token, _, err := s.st.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "注册失败", err.Error())
		return
	}
	s.setSessionCookie(w, token, s.cfg.CookieTTL)
	s.setFlash(w, "注册成功，欢迎加入！")
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// ---- 退出 ----

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if sess := Session(r); sess != nil {
		s.st.DeleteSession(r.Context(), sess.Token)
	}
	s.setSessionCookie(w, "", -1)
	s.setFlash(w, "已退出登录")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- 个人页 ----

func (s *Server) userPage(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r, "id")
	u, err := s.st.UserByID(r.Context(), uid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "用户不存在", "该用户不存在。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	threads, err := s.st.RecentThreadsOfUser(r.Context(), uid, 10)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	var tvms []*store.Thread
	for _, t := range threads {
		tvms = append(tvms, t)
	}
	groupName := "注册会员"
	if u.IsAdmin() {
		groupName = "管理员"
	} else if u.IsModerator() {
		groupName = "版主"
	}
	data := struct {
		Common
		Profile  *store.User
		Group    string
		Trust    string
		Threads  []*store.Thread
		IsSelf   bool
	}{s.common(r), u, groupName, store.TrustLevelName(u.TrustLevel), tvms, User(r) != nil && User(r).ID == u.ID}
	_ = s.rd.Render(w, "page_user.html", &data)
}

func remoteIP(r *http.Request) string {
	for i := 0; i < len(r.RemoteAddr); i++ {
		if r.RemoteAddr[i] == ':' {
			return r.RemoteAddr[:i]
		}
	}
	return r.RemoteAddr
}
