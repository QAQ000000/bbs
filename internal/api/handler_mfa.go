package api

import (
	"dzforum/internal/store"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) mfaGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	v, err := s.st.MFA(r.Context(), User(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		s.respond(w, 200, map[string]any{"enabled": false, "recoveryCodesRemaining": 0})
		return
	}
	if err != nil {
		s.mfaError(w, r, err)
		return
	}
	s.respond(w, 200, map[string]any{"enabled": v.Enabled, "recoveryCodesRemaining": len(v.RecoveryHashes)})
}

func (s *Server) mfaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrMFAUnavailable):
		s.fail(w, r, 503, "MFA_UNAVAILABLE", "二次验证暂不可用，请联系管理员检查密钥配置")
	case errors.Is(err, store.ErrMFALimited):
		s.fail(w, r, 429, "MFA_RATE_LIMITED", "二次验证尝试过多，请稍后重试")
	case errors.Is(err, store.ErrMFAInvalid):
		s.fail(w, r, 401, "MFA_INVALID", "凭据错误或验证已失效")
	default:
		s.fail(w, r, 503, "MFA_FAILED", "二次验证暂不可用，请稍后重试")
	}
}

func (s *Server) mfaSetup(w http.ResponseWriter, r *http.Request)   { s.mfaManage(w, r, "setup") }
func (s *Server) mfaEnable(w http.ResponseWriter, r *http.Request)  { s.mfaManage(w, r, "enable") }
func (s *Server) mfaDisable(w http.ResponseWriter, r *http.Request) { s.mfaManage(w, r, "disable") }
func (s *Server) mfaRecovery(w http.ResponseWriter, r *http.Request) {
	s.mfaManage(w, r, "recovery.rotate")
}

func (s *Server) mfaManage(w http.ResponseWriter, r *http.Request, action string) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "FORBIDDEN", "表单已过期")
		return
	}
	if !s.allow(r, "mfa-manage", 30, 10*time.Minute) {
		s.mfaError(w, r, store.ErrMFALimited)
		return
	}
	out, err := s.st.ManageMFA(r.Context(), User(r).ID, Session(r).ID, action, r.PostFormValue("password"), r.PostFormValue("setupId"), strings.TrimSpace(r.PostFormValue("code")), strings.TrimSpace(r.PostFormValue("recovery")), maskIP(remoteIP(r)), s.mfaCipher)
	if err != nil {
		s.mfaError(w, r, err)
		return
	}
	if action == "setup" {
		account := User(r).Username
		q := url.Values{"secret": {out.Secret}, "issuer": {"GoBBS"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
		uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/GoBBS:" + account, RawQuery: q.Encode()}
		s.respond(w, 200, map[string]any{"secret": out.Secret, "setupId": out.SetupID, "issuer": "GoBBS", "account": account, "otpauthUrl": uri.String(), "expiresIn": 600})
		return
	}
	s.respond(w, 200, out)
}

func mfaBinding(r *http.Request) string {
	if sess := Session(r); sess != nil {
		return sess.CSRF
	}
	if c, err := r.Cookie(cookieCSRF); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) mfaLogin(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "FORBIDDEN", "表单已过期")
		return
	}
	if !s.allow(r, "mfa-login", 30, 10*time.Minute) {
		s.mfaError(w, r, store.ErrMFALimited)
		return
	}
	uid, token, _, err := s.st.CompleteMFA(r.Context(), r.PostFormValue("challenge"), mfaBinding(r), strings.TrimSpace(r.PostFormValue("code")), strings.TrimSpace(r.PostFormValue("recovery")), r.UserAgent(), maskIP(remoteIP(r)), s.mfaCipher)
	if err != nil {
		s.mfaError(w, r, err)
		return
	}
	u, err := s.st.UserByID(r.Context(), uid)
	if err != nil {
		s.mfaError(w, r, err)
		return
	}
	s.st.TouchLogin(r.Context(), uid)
	s.setSessionCookie(w, token, s.cfg.CookieTTL)
	s.respond(w, 200, privateUser(u))
}
