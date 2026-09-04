// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"dzforum/internal/config"
	"dzforum/internal/limiter"
	"dzforum/internal/live"
	"dzforum/internal/mail"
	"dzforum/internal/store"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
	ctxFlash
)

// Server 聚合全部依赖。
type Server struct {
	cfg     config.Config
	st      *store.Store
	rd      *Renderer
	hub     *live.Hub
	log     *slog.Logger
	mux     *http.ServeMux
	handler http.Handler // 装配中间件后的根处理器
	start   time.Time
	pcache  *pageCache
	mailer  *mail.Mailer
	limiter *limiter.Limiter
	prod    bool

	uploadMu     sync.Mutex // 上传目录占用缓存（5 分钟）
	uploadSize   int64
	uploadSizeAt time.Time
}

func New(cfg config.Config, st *store.Store, hub *live.Hub, logger *slog.Logger) (*Server, error) {
	rd, err := NewRenderer(cfg.DevMode)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, st: st, rd: rd, hub: hub, log: logger, mux: http.NewServeMux(),
		start: time.Now(), pcache: newPageCache(60 * time.Second), limiter: limiter.New(),
		mailer: mail.New(mail.Config{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser,
			Password: cfg.SMTPPassword, From: cfg.SMTPFrom,
			SiteName: cfg.SiteName, SiteURL: cfg.SiteURL,
		}, logger),
		prod: cfg.ProdMode}
	s.routes()
	return s, nil
}

// Handler 返回装配完成的根处理器。
func (s *Server) Handler() http.Handler { return s.handler }

// ---- 请求上下文辅助 ----

func User(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxUser).(*store.User)
	return u
}

func Session(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxSession).(*store.Session)
	return s
}

// ---- 通用数据 ----

func (s *Server) common(r *http.Request) Common {
	st := s.sets(r)
	c := Common{
		SiteName:  st.SiteName,
		SiteLogo:  s.cfg.SiteLogo,
		User:      User(r),
		NavActive: r.URL.Path,
		Year:      time.Now().Year(),
	}
	if sess := Session(r); sess != nil {
		c.CSRF = sess.CSRF
	}
	if u := c.User; u != nil {
		c.NotifyCount = s.st.UnreadCount(r.Context(), u.ID)
	}
	c.AssetQuery = assetQuery
	if f, ok := r.Context().Value(ctxFlash).(string); ok {
		c.Flash = f
	}
	return c
}

// checkSiteOpen 关站拦截（后台与登录流程不受限）。
func (s *Server) checkSiteOpen(w http.ResponseWriter, r *http.Request) bool {
	st := s.sets(r)
	if !st.SiteClosed {
		return true
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	data := struct {
		Common
		Title string
		Msg   string
	}{s.common(r), "站点关闭", st.SiteClosedReason}
	_ = s.rd.Render(w, "page_error.html", &data)
	return false
}

// checkNotBanned 禁言拦截（发帖/回帖/编辑路径）。
func (s *Server) checkNotBanned(w http.ResponseWriter, r *http.Request) bool {
	u := User(r)
	if u == nil {
		return true
	}
	if banned, until, reason := s.st.IsBanned(r.Context(), u.ID); banned {
		msg := "您已被禁言至 " + until.Format("2006-01-02 15:04")
		if until.Year() > 9998 {
			msg = "您已被永久禁言"
		}
		if reason != "" {
			msg += "，理由：" + reason
		}
		s.renderError(w, r, http.StatusForbidden, "禁止发言", msg)
		return false
	}
	return true
}

// ---- 会话 Cookie ----

const (
	cookieSession = "forum_session"
	cookieCSRF    = "forum_csrf"
	cookieFlash   = "forum_flash"
)

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieSession, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: int(ttl.Seconds()),
		Secure: s.prod,
	})
}

func (s *Server) setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieFlash, Value: base64.RawURLEncoding.EncodeToString([]byte(msg)),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 60,
	})
}

// anonCSRF 未登录表单用双重提交 Cookie 防 CSRF。
func (s *Server) anonCSRF(r *http.Request, w http.ResponseWriter) string {
	if c, err := r.Cookie(cookieCSRF); err == nil && len(c.Value) >= 20 {
		return c.Value
	}
	tok := newToken(24)
	http.SetCookie(w, &http.Cookie{
		Name: cookieCSRF, Value: tok, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: 86400, Secure: s.prod,
	})
	return tok
}

func (s *Server) checkCSRF(r *http.Request) bool {
	tok := r.PostFormValue("_csrf")
	if tok == "" {
		return false
	}
	if sess := Session(r); sess != nil {
		return tok == sess.CSRF
	}
	c, err := r.Cookie(cookieCSRF)
	return err == nil && c.Value == tok
}

// requireLogin 未登录则跳登录页并记录回跳地址。
func (s *Server) requireLogin(w http.ResponseWriter, r *http.Request) bool {
	if User(r) != nil {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	http.Redirect(w, r, "/login?next="+urlQueryEscape(r.URL.RequestURI()), http.StatusFound)
	return false
}

func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "%", "%25"), "&", "%26")
}

// allow 入口限流判定：action 维度 + 客户端 IP（反代场景回环地址时采信
// X-Real-IP，与部署文档的 nginx 配置配套；直连部署不受影响）。
func (s *Server) allow(r *http.Request, action string, limit int, window time.Duration) bool {
	return s.limiter.Allow(action+":"+s.clientIP(r), limit, window)
}

// allowKey 同上，但使用显式 key（如登录的用户名维度）。
func (s *Server) allowKey(key string, limit int, window time.Duration) bool {
	return s.limiter.Allow(key, limit, window)
}

func (s *Server) clientIP(r *http.Request) string {
	ip := remoteIP(r)
	if isLoopbackIP(ip) {
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return real
		}
	}
	return ip
}

func isLoopbackIP(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "127.")
}
