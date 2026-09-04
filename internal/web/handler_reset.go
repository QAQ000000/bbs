package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---- 忘记密码 / 重置（P1 闭环）----
// 防枚举：无论邮箱是否存在，响应一致；令牌只经邮件传递且库中仅存哈希。

func (s *Server) forgotForm(w http.ResponseWriter, r *http.Request) {
	if User(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	d := struct {
		Common
		CSRF string
		Done bool
	}{s.common(r), s.anonCSRF(r, w), r.URL.Query().Get("done") == "1"}
	d.Title = "找回密码"
	_ = s.rd.Render(w, "page_forgot.html", &d)
}

func (s *Server) forgotSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "forgot", 5, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	email := strings.TrimSpace(r.PostFormValue("email"))
	generic := func() {
		http.Redirect(w, r, "/forgot?done=1", http.StatusSeeOther)
	}
	if email == "" || !s.allowKey("forgotm:"+strings.ToLower(email), 3, time.Hour) {
		generic()
		return
	}
	uid, err := s.st.UIDByEmail(r.Context(), email)
	if err == nil && s.mailer.Enabled() {
		if raw, err := s.st.CreatePasswordReset(r.Context(), uid); err == nil {
			link := s.cfg.SiteURL + "/reset?token=" + url.QueryEscape(raw)
			s.mailer.NotifyPasswordReset(email, link)
		}
	}
	generic()
}

func (s *Server) resetForm(w http.ResponseWriter, r *http.Request) {
	if User(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	token := r.URL.Query().Get("token")
	if _, err := s.st.ResetUIDByToken(r.Context(), token); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "链接无效", "重置链接无效或已过期（有效期 15 分钟）。请重新申请。")
		return
	}
	d := struct {
		Common
		CSRF  string
		Token string
		Error string
	}{s.common(r), s.anonCSRF(r, w), token, ""}
	d.Title = "设置新密码"
	_ = s.rd.Render(w, "page_reset.html", &d)
}

func (s *Server) resetSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "reset", 10, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	token := r.PostFormValue("token")
	password := r.PostFormValue("password")

	fail := func(msg string) {
		d := struct {
			Common
			CSRF  string
			Token string
			Error string
		}{s.common(r), s.anonCSRF(r, w), token, msg}
		d.Title = "设置新密码"
		_ = s.rd.Render(w, "page_reset.html", &d)
	}
	uid, err := s.st.ResetUIDByToken(r.Context(), token)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "链接无效", "重置链接无效或已过期。请重新申请。")
		return
	}
	if len([]rune(password)) < 6 {
		fail("密码至少 6 位")
		return
	}
	if err := s.st.UpdatePassword(r.Context(), uid, password); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "重置失败", err.Error())
		return
	}
	// 令牌作废 + 全部会话下线（旧会话可能已被盗用）
	if err := s.st.ConsumePasswordReset(r.Context(), token); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "重置失败", err.Error())
		return
	}
	s.setFlash(w, "密码已重置，请用新密码登录")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
