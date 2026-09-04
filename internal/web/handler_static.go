// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dzforum/assets"
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

// GET /api/status — 简单健康检查。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true,"ts":"` + time.Now().Format(time.RFC3339) + `"}`))
}
