// SPDX-License-Identifier: AGPL-3.0-or-later
// handler_community.go：收藏/订阅、草稿箱、编辑历史（ROADMAP P2 批次）。
package api

import (
	"net/http"
	"strconv"
)

// ---- 收藏 / 订阅（P2-17）----

// favoriteToggle POST /favorite/{tid}：收藏/取消收藏。
func (s *Server) favoriteToggle(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	tid := pathID(r, "tid")
	if s.visibleThread(w, r, tid) == nil {
		return
	}
	fav, err := s.st.FavoriteToggle(r.Context(), User(r).ID, tid)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	}
	if fav {

	} else {

	}
	uid := User(r).ID
	count := s.st.UnreadFavorites(r.Context(), uid)
	s.publish("u:"+strconv.FormatInt(uid, 10), eventBody{Type: "notify", NotifyCount: int(count)})
	s.respond(w, http.StatusOK, map[string]any{"favorite": fav, "threadId": idString(tid)})
}

// ---- 草稿箱（P2-20）----

// draftDelete POST /drafts/delete：草稿箱删除。
func (s *Server) draftDelete(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	context := r.PostFormValue("context")
	if !validDraftContext(context) {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "草稿上下文无效")
		return

	}
	if err := s.st.DeleteDraft(r.Context(), User(r).ID, context); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		result.Message = "草稿已删除"
	}
	s.respond(w, http.StatusOK, result)
}

// ---- 编辑历史（P2-21）----
