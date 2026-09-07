// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func newToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// chain 由外向内包装中间件。
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// recoverMW 防止 panic 打挂进程。
func (s *Server) recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.log.Error("panic", "err", err, "path", r.URL.Path)
				s.fail(w, r, http.StatusInternalServerError, "", "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logMW 访问日志。
func (s *Server) logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/v1/events" || time.Since(start) > time.Second {
			s.log.Info("req", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
		}
	})
}

// securityMW 基础安全响应头。
func (s *Server) securityMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// gzipMW 对文本类响应做透明压缩（SSE 流式响应跳过）。
func (s *Server) gzipMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.URL.Path == "/api/v1/events" {
			next.ServeHTTP(w, r)
			return
		}
		gw := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gw)
		gzr := &gzipResponseWriter{ResponseWriter: w, w: gw}
		defer func() {
			if gzr.enabled {
				gw.Close()
			}
		}()
		next.ServeHTTP(gzr, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	w       *gzip.Writer
	enabled bool
	decided bool
}

// decide 在首个响应字节前依据 Content-Type 做一次压缩判定；
// 模板渲染走隐式 200，不会触发 WriteHeader，必须在 Write 里兜底。
// Content-Type 缺失时按首字节嗅探 HTML（Go server 自身的 sniff 对
// gzip 字节流会误判，因此启用压缩时必须补上类型）。
func (g *gzipResponseWriter) decide(first []byte) {
	if g.decided {
		return
	}
	g.decided = true
	ct := g.Header().Get("Content-Type")
	if ct == "" {
		return
	}
	compressible := (strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "event-stream")) ||
		strings.Contains(ct, "json") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "svg")
	if compressible {
		g.Header().Del("Content-Length")
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Add("Vary", "Accept-Encoding")
		g.w.Reset(g.ResponseWriter)
		g.enabled = true
	}
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	g.decide(nil)
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	g.decide(b)
	if g.enabled {
		return g.w.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.enabled {
		g.w.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// isStaticPath 无需动态缓存控制的路径。
func isStaticPath(p string) bool {
	for _, pre := range []string{"/uploads/", "/avatar/", "/smiley/"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// authMW 解析会话 Cookie，注入用户/会话/闪现消息。
func (s *Server) authMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
			if sess, err := s.st.Session(r.Context(), c.Value); err == nil {
				if u, err := s.st.UserByID(r.Context(), sess.UserID); err == nil && !u.IsBlocked() {
					r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
					r = r.WithContext(context.WithValue(r.Context(), ctxSession, sess))
					if !isStaticPath(r.URL.Path) {
						_ = s.st.TouchSession(r.Context(), sess.ID, maskIP(remoteIP(r)))
					}

				}
			}
		}
		if !isStaticPath(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// apiStateMW expresses installation and maintenance as JSON, never navigation.
func (s *Server) apiStateMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// Unknown methods/routes are rejected before touching business state.
		_, pattern := s.mux.Handler(r)
		if pattern == "" {
			s.fail(w, r, 405, "METHOD_NOT_ALLOWED", "请求方法不受支持")
			return
		}
		if pattern == "/" {
			s.fail(w, r, 404, "NOT_FOUND", "接口不存在")
			return
		}
		if p == "/api/status" || strings.HasPrefix(p, "/api/v1/health/") || p == "/api/v1/session" || p == "/api/v1/setup" {
			next.ServeHTTP(w, r)
			return
		}
		if s.setupRequired() {
			s.fail(w, r, 409, "SETUP_REQUIRED", "站点尚未初始化")
			return
		}
		if s.sets(r).SiteClosed && !strings.HasPrefix(p, "/api/v1/admin") && !strings.HasPrefix(p, "/api/v1/auth/") && p != "/api/v1/site" && !strings.HasPrefix(p, "/api/v1/me") && !strings.HasPrefix(p, "/captcha/") {
			s.fail(w, r, 503, "SITE_CLOSED", s.sets(r).SiteClosedReason)
			return
		}
		next.ServeHTTP(w, r)
	})
}
