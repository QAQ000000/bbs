// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"dzforum/internal/live"
	"dzforum/internal/store"
)

// ---- SSE 端点 ----

// handleLive 校验通过后按主题建立 SSE 订阅。
// 事件层与页面层同口径：待审核主题仅作者与管理人员可订阅；
// 个人通知主题强制绑定当前会话用户（query 参数仅为显式声明）。
// 版块主题只承载公开口径行，匿名可订阅。
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "sse", 60, time.Minute) {
		s.fail(w, r, http.StatusTooManyRequests, "", "too many requests")
		return
	}
	viewer := User(r)
	var topics []string
	if th := r.URL.Query().Get("thread"); th != "" {
		tid, err := strconv.ParseInt(th, 10, 64)
		if err != nil || tid <= 0 {
			s.fail(w, r, http.StatusBadRequest, "", "bad topic")
			return
		}
		t, err := s.st.Thread(r.Context(), tid)
		if err != nil { // 不存在或已删除：拒绝订阅
			s.fail(w, r, http.StatusForbidden, "", "forbidden")
			return
		}
		if !s.canViewThread(r, t) {
			s.fail(w, r, http.StatusForbidden, "", "forbidden")
			return
		}
		topics = append(topics, "t:"+th)
	}
	if fs := r.URL.Query().Get("forums"); fs != "" {
		for _, f := range splitCSV(fs) {
			fid, err := strconv.ParseInt(f, 10, 64)
			if err != nil || !canReadForum(r, fid) {
				s.fail(w, r, 403, "FORBIDDEN", "无权订阅版块")
				return
			}
			topics = append(topics, "f:"+f)
		}
	}
	if uid := r.URL.Query().Get("user"); uid != "" {
		if viewer == nil || uid != strconv.FormatInt(viewer.ID, 10) {
			s.fail(w, r, http.StatusForbidden, "", "forbidden")
			return
		}
		topics = append(topics, "u:"+uid)
	}
	if len(topics) == 0 {
		s.fail(w, r, http.StatusBadRequest, "", "missing topic")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, r, http.StatusInternalServerError, "", "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")

	sub, cancel := s.hub.Subscribe(32, topics...)
	defer cancel()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(35 * time.Second))
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if !s.liveAuthorized(r) {
				_, _ = fmt.Fprint(w, "data: {\"type\":\"subscription.reset\"}\n\n")
				flusher.Flush()
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(35 * time.Second))
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case payload, ok := <-sub.C():
			if !ok {
				return
			}
			if !s.liveAuthorized(r) {
				_, _ = fmt.Fprint(w, "data: {\"type\":\"subscription.reset\"}\n\n")
				flusher.Flush()
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(35 * time.Second))
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' || r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// ---- 广播：纯数据事件 ----

type eventBody struct {
	Type        string `json:"type"`
	PID         int64  `json:"postId,string,omitempty"`
	TID         int64  `json:"threadId,string,omitempty"`
	FID         int64  `json:"forumId,string,omitempty"`
	Floor       int    `json:"floor,omitempty"`
	Version     int    `json:"version,omitempty"`
	PostCount   int    `json:"postCount,omitempty"`
	LikeCount   int    `json:"likeCount"`
	FromName    string `json:"fromName,omitempty"`
	NotifyCount int    `json:"notifyCount"`
}

func (s *Server) publish(topic string, ev eventBody) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.hub.Publish(live.Event{Topic: topic, Payload: b})
}

func bgCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// Broadcast only identifiers: clients re-fetch authorized data from the API.
func (s *Server) broadcastPost(kind string, th *store.Thread, p *store.Post) {
	s.publish("t:"+strconv.FormatInt(th.ID, 10), eventBody{Type: kind, PID: p.ID, TID: th.ID, FID: th.ForumID, Floor: p.Floor, Version: p.Version, PostCount: th.PostCount})
}
func (s *Server) broadcastThread(kind string, th *store.Thread) {
	if th.Pending {
		return
	}
	s.publish("f:"+strconv.FormatInt(th.ForumID, 10), eventBody{Type: kind, TID: th.ID, FID: th.ForumID})
}

func (s *Server) liveAuthorized(r *http.Request) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	uid, token := int64(0), ""
	if u := User(r); u != nil {
		uid = u.ID
		c, e := r.Cookie(cookieSession)
		if e != nil {
			return false
		}
		token = c.Value
	}
	tid := queryID(r, "thread")
	a, err := s.st.LiveReadAudience(ctx, token, uid, tid)
	if err != nil || !a.SessionValid || a.Settings.SiteClosed {
		return false
	}
	if uid > 0 {
		u, ok := a.Users[uid]
		if !ok || u.Blocked || u.LevelID == nil {
			return false
		}
		if _, ok := a.Config.Level(*u.LevelID); !ok {
			return false
		}
	}
	for _, f := range splitCSV(r.URL.Query().Get("forums")) {
		fid, e := strconv.ParseInt(f, 10, 64)
		if e != nil || !a.CanReadForum(uid, fid) {
			return false
		}
	}
	if tid > 0 && !a.CanReadThread(uid) {
		return false
	}
	return true
}
