package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"dzforum/internal/store"
)

// ---- 忘记密码 / 重置（P1 闭环）----
// 防枚举：无论邮箱是否存在，响应一致；令牌只经邮件传递，队列保存密文。

func (s *Server) forgotSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "forgot", 5, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	email := strings.TrimSpace(r.PostFormValue("email"))
	generic := func() {
		s.respond(w, http.StatusOK, map[string]any{"message": "如果账号邮箱存在，将发送重置邮件"})
	}
	if email == "" || !s.allowKey("forgotm:"+strings.ToLower(email), 3, time.Hour) {
		generic()
		return
	}
	uid, err := s.st.UIDByEmail(r.Context(), email)
	if err == nil && s.mailer.Enabled() {
		if err := s.st.QueueAuthEmail(r.Context(), uid, email, "password_reset", s.mailTokens.Seal); err != nil {
			s.log.Error("password reset email enqueue failed", "uid", uid)
		}
	}
	generic()
}

func (s *Server) resetSubmit(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.allow(r, "reset", 10, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	token := r.PostFormValue("token")
	password := r.PostFormValue("password")

	fail := func(msg string) { s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg) }
	if len([]rune(password)) < 8 {
		fail("密码至少 8 位")
		return
	}
	// 原子重置：令牌条件消费 + 撤销其余重置链接 + 改密 + 全部会话下线
	if _, err := s.st.ResetPasswordByToken(r.Context(), token, password); err != nil {
		if errors.Is(err, store.ErrTokenInvalid) {
			s.fail(w, r, http.StatusBadRequest, "链接无效", "重置链接无效或已过期。请重新申请。")
			return
		}
		s.fail(w, r, http.StatusInternalServerError, "重置失败", err.Error())
		return
	}
	result.Message = "密码已重置，请用新密码登录"
	s.respond(w, http.StatusOK, result)
}
