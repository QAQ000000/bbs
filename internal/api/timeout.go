package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// TimeoutHandler cancels database work and prevents late writes to an expired response.
func (s *Server) timeoutMW(next http.Handler) http.Handler {
	regular, upload := s.cfg.APIRequestTimeout, s.cfg.UploadRequestTimeout
	if regular <= 0 {
		regular = 15 * time.Second
	}
	if upload <= 0 {
		upload = 60 * time.Second
	}
	const body = `{"error":{"code":"REQUEST_TIMEOUT","message":"Request timed out"}}`
	normal := http.TimeoutHandler(next, regular, body)
	uploadHandler := http.TimeoutHandler(next, upload, body)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") || p == "/api/v1/events" {
			next.ServeHTTP(w, r)
			return
		}
		// Exports stream potentially large files and must not be buffered in memory.
		if p == "/api/v1/me/export" {
			ctx, cancel := context.WithTimeout(r.Context(), upload)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if p == "/api/v1/uploads" || p == "/api/v1/me/avatar" {
			uploadHandler.ServeHTTP(w, r)
		} else {
			normal.ServeHTTP(w, r)
		}
	})
}
