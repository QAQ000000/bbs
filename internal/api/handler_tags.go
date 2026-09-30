// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"dzforum/internal/store"
	"net/http"
	"strconv"
	"strings"
)

func tagIDs(r *http.Request) ([]int64, error) {
	raw := r.PostForm["tagIds"]
	if len(raw) > 8 {
		return nil, store.ErrCommunityInvalid
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, v := range raw {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 || seen[id] {
			return nil, store.ErrCommunityInvalid
		}
		ids = append(ids, id)
		seen[id] = true
	}
	return ids, nil
}

func (s *Server) tagsGet(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	rows, total, err := s.st.Tags(r.Context(), page, strings.Contains(r.Pattern, "/admin/"), r.URL.Query().Get("q"))
	if !s.readError(w, r, err) {
		s.list(w, rows, page, 30, total)
	}
}
func (s *Server) tagGet(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "tagId")
	if slug := r.PathValue("slug"); slug != "" {
		var err error
		id, err = s.st.ResolveTag(r.Context(), slug)
		if s.readError(w, r, err) {
			return
		}
	}
	row, err := s.st.Tag(r.Context(), id)
	if !s.readError(w, r, err) {
		// 单资源订阅状态随标签详情返回。
		if u := User(r); u != nil {
			if sub, e := s.st.IsSubscribed(r.Context(), u.ID, row.ID, "tag"); e == nil {
				row.Subscribed = &sub
			}
		}
		s.respond(w, 200, row)
	}
}
func (s *Server) tagSave(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut && pathID(r, "tagId") <= 0 {
		s.communityError(w, r, store.ErrCommunityInvalid)
		return
	}
	v := store.Tag{ID: pathID(r, "tagId"), Name: r.PostFormValue("name"), Slug: r.PostFormValue("slug"), Description: r.PostFormValue("description"), Color: r.PostFormValue("color"), Status: r.PostFormValue("status")}
	v.Version, _ = strconv.Atoi(r.PostFormValue("version"))
	if v.Status == "" && r.Method == http.MethodPost {
		v.Status = "active"
	}
	v, err := s.st.SaveTag(r.Context(), v, User(r).ID)
	if s.communityError(w, r, err) {
		return
	}
	s.logOp(r, "tag.save", "tag #"+idString(v.ID))
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	s.respond(w, status, v)
}
func (s *Server) threadTagsSave(w http.ResponseWriter, r *http.Request) {
	if !s.checkNotBanned(w, r) || !s.checkMustChangePassword(w, r) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	p, err := s.st.Post(r.Context(), th.FirstPostID)
	if s.readError(w, r, err) {
		return
	}
	if !s.memberDecision(r, "post.edit", th.ForumID, p, th).Allowed {
		s.fail(w, r, 403, "FORBIDDEN", "无权编辑主题标签")
		return
	}
	ids, err := tagIDs(r)
	if s.communityError(w, r, err) {
		return
	}
	version, _ := strconv.Atoi(r.PostFormValue("version"))
	if version < 1 {
		s.communityError(w, r, store.ErrCommunityInvalid)
		return
	}
	version, err = s.st.SetThreadTags(r.Context(), th.ID, version, ids)
	if s.communityError(w, r, err) {
		return
	}
	s.broadcastThread("thread.update", th)
	s.respond(w, 200, map[string]any{"version": version, "saved": true})
}
func (s *Server) tagThreads(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "tagId")
	if _, err := s.st.Tag(r.Context(), id); s.readError(w, r, err) {
		return
	}
	page, size := pageOf(r), s.sets(r).ThreadsPerPage
	rows, total, err := s.st.TaggedThreads(r.Context(), id, page, size)
	if s.readError(w, r, err) {
		return
	}
	out, err := s.memberThreadRows(r, rows)
	if !s.readError(w, r, err) {
		s.list(w, out, page, size, total)
	}
}
