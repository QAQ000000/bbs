// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dzforum/assets"
	"dzforum/internal/captcha"
)

var staticFS = assets.Static()

// handleStatic 内嵌静态资源：长缓存；失效靠模板引用的内容指纹版本参数（assetQuery）。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=86400")
	handler := http.StripPrefix("/static/", http.FileServerFS(staticFS))
	handler.ServeHTTP(w, r)
}

// handleFavicon 内嵌 SVG 图标。
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(staticFS, "favicon.svg")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}

// ============ 监控端点 ============

// serveSmiley 自定义表情包图片（管理员导入的运行时数据）。
func (s *Server) serveSmiley(w http.ResponseWriter, r *http.Request) {
	pkg, file := r.PathValue("pkg"), r.PathValue("file")
	// 白名单校验：包名/文件名只允许安全字符，杜绝目录穿越
	if !safeSmileyName(pkg) || !safeSmileyName(file) {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.cfg.SmileyDir, pkg, file)
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, file, time.Now(), f)
}

func safeSmileyName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		ok := r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127
		if !ok {
			return false
		}
	}
	return !strings.Contains(s, "..")
}

// GET /api/status — 健康检查：DB 可达、schema 迁移版本、治理队列积压。
// 面向监控与运维自检；不含连接数等内部细节。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	st := map[string]any{"ok": true, "ts": time.Now().Format(time.RFC3339)}
	v, err := s.st.SchemaVersion(ctx)
	if err != nil {
		st["ok"] = false
		st["db"] = "down"
	} else {
		st["db"] = "up"
		st["schema"] = v
		th, po := s.st.PendingCounts(ctx)
		st["pending"] = map[string]int64{"threads": th, "posts": po, "reports": s.st.OpenReportCount(ctx)}
	}
	b, _ := json.Marshal(st)
	_, _ = w.Write(b)
}

// GET /robots.txt — 抓取规则 + sitemap 指引。
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	body := "User-agent: *\nAllow: /\n"
	if s.cfg.SiteURL != "" {
		body += "Sitemap: " + s.cfg.SiteURL + "/sitemap.xml\n"
	}
	_, _ = w.Write([]byte(body))
}

// GET /captcha/{id} — 算术验证码 SVG（一次性挑战，no-store）。
func (s *Server) captchaImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) < 10 || len(id) > 64 {
		http.NotFound(w, r)
		return
	}
	for _, c := range id {
		ok := c == '_' || c == '-' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			http.NotFound(w, r)
			return
		}
	}
	svg := captcha.Image(id)
	if svg == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(svg))
}
