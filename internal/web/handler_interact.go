// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- 点赞 ----

// POST /api/like/{pid}：切换点赞（仅登录用户，不能赞自己的楼层）。
func (s *Server) likeToggle(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, `{"error":"csrf"}`, http.StatusForbidden)
		return
	}
	if !s.checkNotBanned(w, r) {
		http.Error(w, `{"error":"banned"}`, http.StatusForbidden)
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if p.AuthorID == u.ID {
		http.Error(w, `{"error":"cannot like own post"}`, http.StatusBadRequest)
		return
	}
	liked, count, err := s.st.LikeToggle(r.Context(), pid, u.ID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	s.publish("t:"+strconv.FormatInt(p.ThreadID, 10), eventBody{
		Type: "post.like", PID: pid, LikeCount: count,
	})
	s.bustPageCache()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(map[string]any{"liked": liked, "count": count})
	_, _ = w.Write(b)
}

// ---- 服务端草稿 ----

func validDraftContext(c string) bool {
	return strings.HasPrefix(c, "new:") || strings.HasPrefix(c, "reply:") || strings.HasPrefix(c, "edit:")
}

// POST /api/draft {"context":..., "content":...}
func (s *Server) draftSave(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		Context string `json:"context"`
		Content string `json:"content"`
		CSRF    string `json:"csrf"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validDraftContext(req.Context) {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if sess := Session(r); sess == nil || req.CSRF != sess.CSRF {
		http.Error(w, `{"error":"csrf"}`, http.StatusForbidden)
		return
	}
	if !s.checkNotBanned(w, r) {
		http.Error(w, `{"error":"banned"}`, http.StatusForbidden)
		return
	}
	if len(req.Content) > 40000 {
		http.Error(w, `{"error":"too large"}`, http.StatusRequestEntityTooLarge)
		return
	}
	if err := s.st.SaveDraft(r.Context(), u.ID, req.Context, req.Content); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// GET /api/draft?context=...
func (s *Server) draftGet(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.URL.Query().Get("context")
	if !validDraftContext(ctx) {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	content, updated, err := s.st.Draft(r.Context(), u.ID, ctx)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(map[string]any{
		"content":   content,
		"updatedAt": updated.Unix(),
	})
	_, _ = w.Write(b)
}

// ---- 游客整页缓存----

type cacheEntry struct {
	body    []byte
	expires time.Time
}

type pageCache struct {
	mu  sync.Mutex
	m   map[string]cacheEntry
	ttl time.Duration
}

func newPageCache(ttl time.Duration) *pageCache {
	return &pageCache{m: map[string]cacheEntry{}, ttl: ttl}
}

func (c *pageCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expires) {
		if ok {
			delete(c.m, key)
		}
		return nil, false
	}
	return e.body, true
}

func (c *pageCache) put(key string, body []byte) {
	c.mu.Lock()
	c.m[key] = cacheEntry{body: body, expires: time.Now().Add(c.ttl)}
	if len(c.m) > 500 { // 防膨胀：超限整体清空
		c.m = map[string]cacheEntry{}
	}
	c.mu.Unlock()
}

func (c *pageCache) flush() {
	c.mu.Lock()
	c.m = map[string]cacheEntry{}
	c.mu.Unlock()
}

func (s *Server) bustPageCache() { s.pcache.flush() }

var crawlerHints = []string{"bot", "spider", "crawler", "Baiduspider", "Googlebot", "bingbot",
	"Sogou", "360Spider", "YisouSpider", "DuckDuckBot", "slurp"}

func isCrawlerUA(ua string) bool {
	for _, h := range crawlerHints {
		if strings.Contains(strings.ToLower(ua), strings.ToLower(h)) {
			return true
		}
	}
	return false
}

type captureWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (c *captureWriter) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(b []byte) (int, error) {
	c.buf.Write(b)
	return c.ResponseWriter.Write(b)
}

// cachedPage 游客页面缓存中间件：登录用户、带闪现消息的请求、非 GET 一律旁路；
// 爬虫 UA 单独缓存键（同一份 SSR HTML，本就无 JS 依赖）。
func (s *Server) cachedPage(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || User(r) != nil {
			next(w, r)
			return
		}
		if _, err := r.Cookie(cookieFlash); err == nil {
			next(w, r) // 闪现消息属于个人会话，不缓存
			return
		}
		key := r.URL.RequestURI()
		if isCrawlerUA(r.UserAgent()) {
			key = "crawl|" + key
		} else {
			key = "anon|" + key
		}
		if body, ok := s.pcache.get(key); ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Page-Cache", "hit")
			_, _ = w.Write(body)
			return
		}
		cw := &captureWriter{ResponseWriter: w}
		next(cw, r)
		if cw.status == 0 {
			cw.status = 200
		}
		if cw.status == http.StatusOK && cw.buf.Len() > 0 && cw.buf.Len() < 1<<20 {
			s.pcache.put(key, cw.buf.Bytes())
		}
	}
}
