// SPDX-License-Identifier: AGPL-3.0-or-later
// handler_community.go：收藏/订阅、草稿箱、编辑历史（ROADMAP P2 批次）。
package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// ---- 收藏 / 订阅（P2-17）----

// favoriteToggle POST /favorite/{tid}：收藏/取消收藏。
func (s *Server) favoriteToggle(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	tid := pathID(r, "tid")
	if _, err := s.st.Thread(r.Context(), tid); errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "主题不存在", "该主题不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	}
	fav, err := s.st.FavoriteToggle(r.Context(), User(r).ID, tid)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	}
	if fav {
		s.setFlash(w, "已收藏，有新回复时可在「我的收藏」看到提醒")
	} else {
		s.setFlash(w, "已取消收藏")
	}
	uid := User(r).ID
	count := s.st.UnreadFavorites(r.Context(), uid)
	s.publish("u:"+strconv.FormatInt(uid, 10), eventBody{Type: "notify", NotifyCount: int(count)})
	http.Redirect(w, r, ThreadURL(tid, 1), http.StatusSeeOther)
}

// favoritesPage GET /favorites：我的收藏（含未读新回复标记）。
func (s *Server) favoritesPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	const size = 20
	u := User(r)
	rows, total, err := s.st.FavoritesOfUser(r.Context(), u.ID, page, size)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if rows == nil {
		rows = []*store.FavoriteRow{}
	}
	totalPage := (total + size - 1) / size
	if totalPage < 1 {
		totalPage = 1
	}
	data := struct {
		Common
		Rows      []*store.FavoriteRow
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.common(r), rows, nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string {
		return "/favorites?page=" + strconv.Itoa(n)
	})
	data.Title = "我的收藏"
	data.NavActive = "favorites"
	_ = s.rd.Render(w, "page_favorites.html", &data)
}

// ---- 草稿箱（P2-20）----

func (s *Server) draftsPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	raw, err := s.st.DraftsOfUser(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	type draftRow struct {
		Label, Link, Excerpt, Context string
		UpdatedAt                     time.Time
	}
	rows := make([]draftRow, 0, len(raw))
	for _, d := range raw {
		label, link := store.DraftContextLabel(d.Context)
		rows = append(rows, draftRow{Label: label, Link: link,
			Excerpt: truncate(d.Content, 60), Context: d.Context, UpdatedAt: d.UpdatedAt})
	}
	data := struct {
		Common
		Rows []draftRow
	}{s.common(r), rows}
	data.Title = "草稿箱"
	data.NavActive = "drafts"
	_ = s.rd.Render(w, "page_drafts.html", &data)
}

// draftDelete POST /drafts/delete：草稿箱删除。
func (s *Server) draftDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	context := r.PostFormValue("context")
	if !validDraftContext(context) {
		s.setFlash(w, "草稿上下文无效")
		http.Redirect(w, r, "/drafts", http.StatusSeeOther)
		return
	}
	if err := s.st.DeleteDraft(r.Context(), User(r).ID, context); err != nil {
		s.setFlash(w, "删除失败："+err.Error())
	} else {
		s.setFlash(w, "草稿已删除")
	}
	http.Redirect(w, r, "/drafts", http.StatusSeeOther)
}

// ---- 编辑历史（P2-21）----

// postHistory GET /post/{pid}/history：楼层编辑历史（作者与版主/管理员可见）。
func (s *Server) postHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "内容不存在", "该楼层不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	u := User(r)
	if !canEditContent(u, p.AuthorID) && !hasPoint(u, perm.ContentModerate) {
		s.renderError(w, r, http.StatusForbidden, "没有权限", "只有作者与版主可以查看编辑历史。")
		return
	}
	edits, err := s.st.PostEditsOf(r.Context(), pid)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if edits == nil {
		edits = []*store.PostEdit{}
	}
	data := struct {
		Common
		Post  *store.Post
		Edits []*store.PostEdit
	}{s.common(r), p, edits}
	data.Title = "编辑历史"
	_ = s.rd.Render(w, "page_history.html", &data)
}
