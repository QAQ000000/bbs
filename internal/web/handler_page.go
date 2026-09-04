// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dzforum/internal/store"
)

// ---- 首页 ----

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	cats, err := s.st.CategoriesWithForums(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	stats, err := s.st.SiteStats(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	announces, _ := s.st.Announcements(r.Context(), true, 3)
	data := struct {
		Common
		Stats         store.SiteStats
		Categories    []*store.Category
		Announcements []*store.Announcement
		Topics        string
	}{s.common(r), stats, cats, announces, homeTopics(cats)}
	data.NavActive = "home"
	data.Title = ""
	_ = s.rd.Render(w, "page_home.html", &data)
}

func homeTopics(cats []*store.Category) string {
	t := "forums="
	first := true
	for _, c := range cats {
		for _, f := range c.Forums {
			if !first {
				t += ","
			}
			t += strconv.FormatInt(f.ID, 10)
			first = false
		}
	}
	return t
}

// ---- 版块页 forum-N-P.html ----

func (s *Server) handleForum(w http.ResponseWriter, r *http.Request, fid int64, page int) {
	forum, err := s.st.Forum(r.Context(), fid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "版块不存在", "该版块不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}

	stickies, err := s.st.Stickies(r.Context(), fid)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	threads, total, err := s.st.Threads(r.Context(), fid, page, s.sets(r).ThreadsPerPage)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}

	perPage := s.sets(r).ThreadsPerPage
	totalPages := (total + perPage - 1) / perPage
	fidInt := fid
	pager := BuildPage(page, totalPages, func(n int) string { return ForumURL(fidInt, n) })

	data := struct {
		Common
		Forum     *store.Forum
		Stickies  []*store.Thread
		Threads   []*store.Thread
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.common(r), forum, stickies, threads, pager, page, totalPages}
	data.Title = forum.Name
	data.NavActive = "forum" + strconv.FormatInt(fid, 10)
	_ = s.rd.Render(w, "page_forum.html", &data)
}

// ---- 帖子页 thread-N-P-L.html ----

func (s *Server) handleThread(w http.ResponseWriter, r *http.Request, tid int64, page int) {
	th, err := s.st.Thread(r.Context(), tid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "主题不存在", "该主题不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}

	forum, err := s.st.Forum(r.Context(), th.ForumID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}

	perPage := s.sets(r).PostsPerPage
	totalPages := (th.PostCount + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		http.Redirect(w, r, ThreadURL(tid, totalPages)+"#last", http.StatusFound)
		return
	}

	// 待审核主题：仅作者与管理人员可见
	viewer := User(r)
	viewerIsStaff := isStaff(viewer)
	isAuthor := viewer != nil && viewer.ID == th.AuthorID
	if th.Pending && !viewerIsStaff && !isAuthor {
		s.renderError(w, r, http.StatusNotFound, "主题不存在", "该主题不存在或正在等待审核。")
		return
	}
	includePending := viewerIsStaff || isAuthor
	posts, err := s.st.Posts(r.Context(), tid, page, s.sets(r).PostsPerPage, includePending)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}

	s.st.IncView(tid)
	if viewer != nil {
		maxFloor := (page-1)*s.sets(r).PostsPerPage + len(posts)
		if maxFloor > th.PostCount {
			maxFloor = th.PostCount
		}
		s.st.RecordRead(r.Context(), viewer.ID, tid, maxFloor)
		s.st.MaybeUpgradeTrust(r.Context(), viewer.ID)
	}

	common := s.common(r)
	pvm := make([]*PostVM, 0, len(posts))
	for _, p := range posts {
		pvm = append(pvm, PostVMOf(p, viewer, common.CSRF))
	}

	tidInt := tid
	pager := BuildPage(page, totalPages, func(n int) string { return ThreadURL(tidInt, n) })

	sets := s.sets(r)
	var quick *editorData
	if viewer != nil && !th.Closed {
		quick = &editorData{
			Common:        common,
			Action:        "/reply/" + strconv.FormatInt(tid, 10),
			Thread:        th,
			Smileys:       SmileyGroups(),
			EditorID:      "q",
			DraftContext:  "reply:" + strconv.FormatInt(tid, 10),
			UploadEnabled: sets.UploadEnabled,
			MaxImageMB:    sets.MaxImageMB,
			MaxFileMB:     sets.MaxFileMB,
		}
	}

	data := struct {
		Common
		Thread       *store.Thread
		Forum        *store.Forum
		Posts        []*PostVM
		Page         []PageItem
		PageNum      int
		TotalPage    int
		LiveTopic    string
		QuickReply   *editorData
		PostsPerPage int
	}{common, th, forum, pvm, pager, page, totalPages,
		"thread=" + strconv.FormatInt(tid, 10), quick, perPage}
	data.Title = th.Title
	_ = s.rd.Render(w, "page_thread.html", &data)
}

// ---- 全文搜索 ----

func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	var hits []*store.SearchHit
	var total int
	if q != "" {
		if !s.allow(r, "search", 30, time.Minute) {
			s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "搜索太快了，请稍后再试。")
			return
		}
		var err error
		hits, total, err = s.st.Search(r.Context(), q, page, 20)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "搜索失败", err.Error())
			return
		}
	}
	totalPage := (total + 19) / 20
	if totalPage < 1 {
		totalPage = 1
	}
	data := struct {
		Common
		Q         string
		Hits      []*store.SearchHit
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.common(r), q, hits, nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string {
		return "/search?q=" + urlQueryEscape(q) + "&page=" + strconv.Itoa(n)
	})
	data.Title = "搜索：" + q
	_ = s.rd.Render(w, "page_search.html", &data)
}
