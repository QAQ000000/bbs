// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"net/http"
	"strconv"
	"strings"
)

// ---- 点赞 ----

// POST /api/like/{pid}：切换点赞（仅登录用户，不能赞自己的楼层）。
func (s *Server) likeToggle(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		s.fail(w, r, http.StatusUnauthorized, "", `{"error":"unauthorized"}`)
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "", `{"error":"csrf"}`)
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	pid := pathID(r, "pid")
	p, _ := s.visiblePost(w, r, pid)
	if p == nil {
		return
	}
	if p.AuthorID == u.ID {
		s.fail(w, r, http.StatusBadRequest, "", `{"error":"cannot like own post"}`)
		return
	}
	liked, count, err := s.st.LikeToggle(r.Context(), pid, u.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	s.publish("t:"+strconv.FormatInt(p.ThreadID, 10), eventBody{
		Type: "post.like", PID: pid, LikeCount: count,
	})

	s.respond(w, 200, map[string]any{"liked": liked, "count": count})
}

// ---- 服务端草稿 ----

func validDraftContext(c string) bool {
	return strings.HasPrefix(c, "new:") || strings.HasPrefix(c, "reply:") || strings.HasPrefix(c, "edit:")
}

// POST /api/draft {"context":..., "content":...}
func (s *Server) draftSave(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		s.fail(w, r, http.StatusUnauthorized, "", `{"error":"unauthorized"}`)
		return
	}
	req := struct{ Context, Content string }{r.PostFormValue("context"), r.PostFormValue("content")}
	if !validDraftContext(req.Context) {
		s.fail(w, r, 400, "BAD_REQUEST", "草稿上下文无效")
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "CSRF_INVALID", "CSRF token 无效")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	if len(req.Content) > 40000 {
		s.fail(w, r, http.StatusRequestEntityTooLarge, "", `{"error":"too large"}`)
		return
	}
	if err := s.st.SaveDraft(r.Context(), u.ID, req.Context, req.Content); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	s.respond(w, 200, map[string]bool{"saved": true})
}

// GET /api/draft?context=...
func (s *Server) draftGet(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		s.fail(w, r, http.StatusUnauthorized, "", `{"error":"unauthorized"}`)
		return
	}
	ctx := r.URL.Query().Get("context")
	if !validDraftContext(ctx) {
		s.fail(w, r, http.StatusBadRequest, "", `{"error":"bad request"}`)
		return
	}
	content, updated, err := s.st.Draft(r.Context(), u.ID, ctx)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	s.respond(w, 200, map[string]any{"content": content, "updatedAt": updated})
}
