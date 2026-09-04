// SPDX-License-Identifier: AGPL-3.0-or-later
package web

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
				http.Error(w, "服务器内部错误", http.StatusInternalServerError)
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
		if r.URL.Path != "/api/live" || time.Since(start) > time.Second {
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
		// 样式允许内联（模板含少量 style 属性）；脚本全部为外链文件
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// gzipMW 对文本类响应做透明压缩（SSE 流式响应跳过）。
func (s *Server) gzipMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.URL.Path == "/api/live" {
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
		if len(first) > 0 && first[0] == '<' {
			ct = "text/html; charset=utf-8"
			g.Header().Set("Content-Type", ct)
		} else {
			return
		}
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
	for _, pre := range []string{"/static/", "/uploads/", "/avatar/", "/smiley/", "/favicon.ico", "/robots.txt"} {
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
			if sess, err := s.st.SessionCached(r.Context(), c.Value); err == nil {
				if u, err := s.st.UserByID(r.Context(), sess.UserID); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
					r = r.WithContext(context.WithValue(r.Context(), ctxSession, sess))
					uid := u.ID
					go func() {
						bg, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()
						s.st.TouchVisit(bg, uid) // 当日访问打点（内存去重）
					}()
				}
			}
		}
		if !isStaticPath(r.URL.Path) {
			if User(r) != nil {
				w.Header().Set("Cache-Control", "no-store") // 登录态页面禁止本地缓存
			} else {
				w.Header().Set("Cache-Control", "no-cache") // 匿名动态页：允许存储但需回源
			}
		}
		if c, err := r.Cookie(cookieFlash); err == nil && c.Value != "" {
			if b, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxFlash, string(b)))
				http.SetCookie(w, &http.Cookie{Name: cookieFlash, Value: "", Path: "/", MaxAge: -1})
			}
		}
		next.ServeHTTP(w, r)
	})
}
