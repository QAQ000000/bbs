// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/store"
)

// ---- 个人设置（ROADMAP 阶段一）：签名/邮箱资料 + 改密（撤销其他会话）----

type profilePage struct {
	Common
	Profile    *store.User
	Section    string // 出错表单："profile" | "password"，空则无错误
	Error      string
	Signature  string
	Email      string
	EmailGate  bool // 邮箱验证闸门生效中（显示验证提示）
	MustChange bool // 初始密码未改（-seed 账号强制改密）
}

func (s *Server) renderProfile(w http.ResponseWriter, r *http.Request, d profilePage) {
	d.Common = s.common(r)
	d.Title = "资料设置"
	d.EmailGate = s.emailGateEnabled()
	if u := User(r); u != nil && u.MustChangePassword {
		d.MustChange = true
	}
	_ = s.rd.Render(w, "page_profile.html", &d)
}

// profileForm GET /profile：资料与改密表单。
func (s *Server) profileForm(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	s.renderProfile(w, r, profilePage{
		Profile:   u,
		Signature: u.Signature,
		Email:     u.Email,
	})
}

// profileSave POST /profile/save：签名 + 邮箱。
func (s *Server) profileSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allow(r, "profile-save", 10, time.Minute) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	signature := strings.TrimSpace(r.PostFormValue("signature"))
	email := strings.TrimSpace(r.PostFormValue("email"))

	fail := func(msg string) {
		s.renderProfile(w, r, profilePage{Profile: u, Section: "profile",
			Error: msg, Signature: signature, Email: email})
	}
	if utf8.RuneCountInString(signature) > 200 {
		fail("签名不能超过 200 字")
		return
	}
	if email != "" && !emailRe.MatchString(email) {
		fail("邮箱格式不正确")
		return
	}
	if email != "" {
		if other, err := s.st.UIDByEmail(r.Context(), email); err == nil && other != u.ID {
			fail("该邮箱已被其他账号使用")
			return
		}
	}
	if err := s.st.UpdateProfile(r.Context(), u.ID, signature, email); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	s.setFlash(w, "资料已保存")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profilePassword POST /profile/password：旧密码验证 + 撤销其他会话。
func (s *Server) profilePassword(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	// 旧密码验证是唯一闸门：限流防在线爆破
	if !s.allow(r, "profile-pw", 10, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "密码修改尝试过多，请一小时后再试。")
		return
	}
	old := r.PostFormValue("old_password")
	new := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	fail := func(msg string) {
		s.renderProfile(w, r, profilePage{Profile: u, Section: "password", Error: msg})
	}
	if utf8.RuneCountInString(new) < 8 {
		fail("新密码至少 8 位")
		return
	}
	if new != confirm {
		fail("两次输入的新密码不一致")
		return
	}
	if new == old {
		fail("新密码不能与当前密码相同")
		return
	}
	// Cookie 里是原始 token；留空则撤销全部会话（防御分支，正常必非空）
	raw := ""
	if c, err := r.Cookie(cookieSession); err == nil {
		raw = c.Value
	}
	if err := s.st.ChangePassword(r.Context(), u.ID, old, new, raw); err != nil {
		if errors.Is(err, store.ErrWrongPassword) {
			fail("当前密码不正确")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "修改失败", err.Error())
		return
	}
	s.setFlash(w, "密码已修改，其他设备已退出登录")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profileVerifyResend POST /profile/verify-resend：重发验证邮件（3 次/小时）。
func (s *Server) profileVerifyResend(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.emailGateEnabled() || u.Email == "" || u.EmailVerified {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	if !s.allowKey("verify:"+strconv.FormatInt(u.ID, 10), 3, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "验证邮件发送过于频繁，请一小时后再试。")
		return
	}
	if raw, err := s.st.CreateEmailVerify(r.Context(), u.ID); err == nil {
		link := s.cfg.SiteURL + "/verify?token=" + url.QueryEscape(raw)
		s.mailer.NotifyEmailVerify(u.Email, link)
	}
	s.setFlash(w, "验证邮件已发送，请查收（24 小时内有效）")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}
