// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/captcha"
	"dzforum/internal/perm"
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
	// 反爆破：IP 与用户名双维度限流（在线爆破由本层拦截，bcrypt 只防离线）
	if !s.allow(r, "login", 15, 10*time.Minute) ||
		!s.allowKey("loginu:"+strings.ToLower(strings.TrimSpace(r.PostFormValue("username"))), 8, 10*time.Minute) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "登录尝试过多，请 10 分钟后再试。")
		return
	}
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
		s.st.AdminLog(r.Context(), 0, "", "login.fail", "登录失败：用户名或密码不正确", remoteIP(r))
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
	if hasPoint(u, perm.AdminPanel) {
		s.st.AdminLog(r.Context(), u.ID, u.Username, "login.ok", "管理员登录", remoteIP(r))
	}
	s.st.TouchLogin(r.Context(), u.ID)
	s.setSessionCookie(w, token, s.cfg.CookieTTL)
	if u.MustChangePassword {
		// -seed 初始账号：首次登录强制进资料设置改密
		s.setFlash(w, "首次登录：请先修改初始密码")
		http.Redirect(w, r, "/profile?must=1", http.StatusSeeOther)
		return
	}
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
	d := s.registerPageData(w, r, nextOf(r), "", "", "")
	_ = s.rd.Render(w, "page_register.html", &d)
}

// registerPageData 注册页数据；captcha 开启时生成新挑战。
func (s *Server) registerPageData(w http.ResponseWriter, r *http.Request, next, errMsg, username, email string) struct {
	Common
	Next           string
	CSRF           string
	Error          string
	Username       string
	Email          string
	CaptchaEnabled bool
	CaptchaID      string
	RequireConsent bool
} {
	d := struct {
		Common
		Next           string
		CSRF           string
		Error          string
		Username       string
		Email          string
		CaptchaEnabled bool
		CaptchaID      string
		RequireConsent bool
	}{s.common(r), next, s.anonCSRF(r, w), errMsg, username, email,
		s.sets(r).CaptchaEnabled, "", s.sets(r).RequireConsent}
	if d.CaptchaEnabled {
		d.CaptchaID, _ = captcha.New()
	}
	return d
}

// reservedName 保留用户名（ROADMAP 阶段四）：精确命中或前缀命中均拒绝。
var reservedNameExact = map[string]bool{
	"admin": true, "administrator": true, "root": true, "system": true,
	"moderator": true, "staff": true, "owner": true, "official": true, "guest": true,
	"管理员": true, "版主": true, "官方": true, "系统": true, "站务": true,
}

var reservedNamePrefixes = []string{"admin", "moderator", "gobbs", "official"}

func reservedName(name string) bool {
	low := strings.ToLower(name)
	if reservedNameExact[low] {
		return true
	}
	for _, p := range reservedNamePrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

func (s *Server) registerSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.sets(r).RegisterEnabled {
		s.renderError(w, r, http.StatusForbidden, "注册已关闭", "本站已关闭新用户注册，请联系管理员。")
		return
	}
	// 反批量注册：单 IP 每小时 5 次、每天 20 次
	if !s.allow(r, "reg", 5, time.Hour) || !s.allow(r, "regd", 20, 24*time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "注册尝试过多，请稍后再试。")
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

	fail := func(msg string) {
		d := s.registerPageData(w, r, next, msg, username, email)
		_ = s.rd.Render(w, "page_register.html", &d)
	}
	if !usernameRe.MatchString(username) {
		fail("用户名需为 2-15 位中文、字母、数字或下划线")
		return
	}
	if reservedName(username) {
		fail("该用户名为系统保留，请换一个")
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
	// 条款与隐私勾选（后台开关，ROADMAP 阶段六）
	if s.sets(r).RequireConsent && r.PostFormValue("consent") != "1" {
		fail("请先阅读并同意服务条款与隐私政策")
		return
	}
	// 算术验证码（后台开关；挑战一次性，失败需换新题）
	if s.sets(r).CaptchaEnabled &&
		!captcha.Verify(r.PostFormValue("captcha_id"), strings.TrimSpace(r.PostFormValue("captcha"))) {
		fail("验证码不正确，请输入图片中算式的结果")
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
	// 邮箱验证开关开启且邮件可用：注册即发验证邮件（24h 有效）
	if s.emailGateEnabled() && email != "" {
		if raw, err := s.st.CreateEmailVerify(r.Context(), u.ID); err == nil {
			link := s.cfg.SiteURL + "/verify?token=" + url.QueryEscape(raw)
			s.mailer.NotifyEmailVerify(email, link)
		}
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

// verifyEmail GET /verify?token=...：邮箱验证一次性消费。
func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "verify", 30, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if _, err := s.st.ConsumeEmailVerify(r.Context(), r.URL.Query().Get("token")); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "链接无效",
			"验证链接无效或已过期（有效期 24 小时）。请在资料设置中重发验证邮件。")
		return
	}
	s.setFlash(w, "邮箱验证成功")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- 退出 ----

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	// Cookie 里是原始 token，DeleteSession 内部做哈希；不能传 sess.Token（已是哈希，会二次哈希删不掉）
	if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
		s.st.DeleteSession(r.Context(), c.Value)
	}
	s.setSessionCookie(w, "", -1)
	s.setFlash(w, "已退出登录")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// checkMustChangePassword 初始密码未改的写入口拦截（-seed 账号强制改密，ROADMAP 5.1）。
func (s *Server) checkMustChangePassword(w http.ResponseWriter, r *http.Request) bool {
	if u := User(r); u != nil && u.MustChangePassword {
		s.renderError(w, r, http.StatusForbidden, "请先修改初始密码",
			"检测到您仍在使用初始密码，请先在「资料设置」中修改密码，再进行此操作。")
		return false
	}
	return true
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
	replies, err := s.st.RecentRepliesOfUser(r.Context(), uid, 10)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if threads == nil {
		threads = []*store.Thread{}
	}
	if replies == nil {
		replies = []*store.Thread{}
	}
	groupName := perm.RoleName(perm.RoleFromGroupID(u.GroupID))
	data := struct {
		Common
		Profile *store.User
		Group   string
		Trust   string
		Threads []*store.Thread
		Replies []*store.Thread
		IsSelf  bool
	}{s.common(r), u, groupName, store.TrustLevelName(u.TrustLevel), threads, replies, User(r) != nil && User(r).ID == u.ID}
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
