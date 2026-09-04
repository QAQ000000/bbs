// SPDX-License-Identifier: AGPL-3.0-or-later
package web

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

// canViewPending 待审核主题的可见性：作者本人或管理人员（与帖子页同口径）。
func canViewPending(viewer *store.User, th *store.Thread) bool {
	return viewer != nil && (viewer.IsStaff() || viewer.ID == th.AuthorID)
}

// ---- SSE 端点 ----

// handleLive 校验通过后按主题建立 SSE 订阅。
// 事件层与页面层同口径：待审核主题仅作者与管理人员可订阅；
// 个人通知主题强制绑定当前会话用户（query 参数仅为显式声明）。
// 版块主题只承载公开口径行，匿名可订阅。
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "sse", 60, time.Minute) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	viewer := User(r)
	var topics []string
	if th := r.URL.Query().Get("thread"); th != "" {
		tid, err := strconv.ParseInt(th, 10, 64)
		if err != nil || tid <= 0 {
			http.Error(w, "bad topic", http.StatusBadRequest)
			return
		}
		t, err := s.st.Thread(r.Context(), tid)
		if err != nil { // 不存在或已删除：拒绝订阅
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if t.Pending && !canViewPending(viewer, t) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		topics = append(topics, "t:"+th)
	}
	if fs := r.URL.Query().Get("forums"); fs != "" {
		for _, f := range splitCSV(fs) {
			topics = append(topics, "f:"+f)
		}
	}
	if uid := r.URL.Query().Get("user"); uid != "" {
		if viewer == nil || uid != strconv.FormatInt(viewer.ID, 10) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		topics = append(topics, "u:"+uid)
	}
	if len(topics) == 0 {
		http.Error(w, "missing topic", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
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
			_ = rc.SetWriteDeadline(time.Now().Add(35 * time.Second))
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case payload, ok := <-sub.C():
			if !ok {
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

// ---- 广播：事件载荷与 HTML 片段 ----

type eventBody struct {
	Type      string `json:"type"`                // post.new / post.edit / post.delete / thread.new / thread.update / thread.delete
	PID       int64  `json:"pid,omitempty"`
	Floor     int    `json:"floor,omitempty"`
	PostCount int    `json:"postCount,omitempty"` // 主题总楼层数（客户端算末页）
	TID       int64  `json:"tid,omitempty"`
	PostHTML  string `json:"postHtml,omitempty"`  // 楼层片段 HTML
	LikeCount int    `json:"likeCount,omitempty"` // 点赞后最新计数
	FromName  string `json:"fromName,omitempty"`  // 通知来源用户
	NotifyCount int  `json:"notifyCount,omitempty"` // 未读通知总数
	ThreadRow string `json:"threadRow,omitempty"` // 版块页主题行 HTML
	ForumRow  string `json:"forumRow,omitempty"`  // 首页版块行 HTML
}

func (s *Server) publish(topic string, ev eventBody) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.hub.Publish(live.Event{Topic: topic, Payload: b})
}

func bgCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel // 进程生命周期内很短，交给超时兜底
	return ctx
}

// broadcastPost 楼层事件：新楼 / 编辑 / 删除。
func (s *Server) broadcastPost(evType string, th *store.Thread, p *store.Post) {
	s.bustPageCache()
	html := ""
	if evType != "post.delete" {
		html, _ = s.rd.RenderPartial("post_floor", PostVMOf(p, nil, ""))
	}
	s.publish("t:"+strconv.FormatInt(th.ID, 10), eventBody{
		Type: evType, PID: p.ID, Floor: p.Floor,
		PostCount: th.PostCount, TID: th.ID, PostHTML: html,
		ThreadRow: s.threadRowHTML(th),
	})
}

// broadcastThread 主题行事件（版块页 + 首页同步刷新）。
func (s *Server) broadcastThread(evType string, th *store.Thread) {
	s.bustPageCache()
	s.publish("f:"+strconv.FormatInt(th.ForumID, 10), eventBody{
		Type: evType, TID: th.ID,
		ThreadRow: s.threadRowHTML(th),
		ForumRow:  s.forumRowHTML(th.ForumID),
	})
}

func (s *Server) threadRowHTML(th *store.Thread) string {
	html, err := s.rd.RenderPartial("thread_row", th)
	if err != nil {
		return ""
	}
	return html
}

func (s *Server) forumRowHTML(fid int64) string {
	f, err := s.st.Forum(bgCtx(), fid)
	if err != nil {
		return ""
	}
	html, err := s.rd.RenderPartial("forum_row", f)
	if err != nil {
		return ""
	}
	return html
}
