// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dzforum/internal/markdown"
	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// ---- 首页 ----

// latestItem 最新主题条目：主题行 + 版块名（首页「最新回复」与 /latest 共用）。
type latestItem struct {
	*store.Thread
	ForumName string
}

func forumNameMap(cats []*store.Category) map[int64]string {
	m := map[int64]string{}
	for _, c := range cats {
		for _, f := range c.Forums {
			m[f.ID] = f.Name
		}
	}
	return m
}

func (s *Server) latestItems(r *http.Request, page, size int) ([]latestItem, int, error) {
	threads, total, err := s.st.LatestThreads(r.Context(), page, size)
	if err != nil {
		return nil, 0, err
	}
	cats, _ := s.st.CategoriesWithForums(r.Context())
	names := forumNameMap(cats)
	items := make([]latestItem, 0, len(threads))
	for _, t := range threads {
		items = append(items, latestItem{t, names[t.ForumID]})
	}
	return items, total, nil
}

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
	recent, _, err := s.latestItems(r, 1, 10)
	if err != nil {
		recent = []latestItem{}
	}
	data := struct {
		Common
		Stats         store.SiteStats
		Categories    []*store.Category
		Announcements []*store.Announcement
		Recent        []latestItem
		Topics        string
	}{s.common(r), stats, cats, announces, recent, homeTopics(cats)}
	data.NavActive = "home"
	data.Title = ""
	_ = s.rd.Render(w, "page_home.html", &data)
}

// ---- 全站最新 ----

func (s *Server) latestPage(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	const size = 20
	items, total, err := s.latestItems(r, page, size)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	totalPage := (total + size - 1) / size
	if totalPage < 1 {
		totalPage = 1
	}
	if page > totalPage {
		http.Redirect(w, r, "/latest?page="+strconv.Itoa(totalPage), http.StatusFound)
		return
	}
	data := struct {
		Common
		Items     []latestItem
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.common(r), items, nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string { return "/latest?page=" + strconv.Itoa(n) })
	data.Title = "最新回复"
	data.NavActive = "latest"
	_ = s.rd.Render(w, "page_latest.html", &data)
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
	data.MetaDesc = forum.Description
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
	// 版主删除限管辖版块（管理员不限；删自己的内容始终允许）
	if viewer != nil && !hasPoint(viewer, perm.AdminPanel) {
		inScope := inForumScope(s.staffForumScope(r), forum.ID)
		for _, vm := range pvm {
			if vm.AuthorID != viewer.ID {
				vm.Deletable = inScope &&
					perm.Allowed(perm.RoleFromGroupID(viewer.GroupID), perm.ContentDeleteAny)
			}
		}
	}

	tidInt := tid
	pager := BuildPage(page, totalPages, func(n int) string { return ThreadURL(tidInt, n) })

	sets := s.sets(r)
	var quick *editorData
	if viewer != nil && !th.Closed {
		if banned, _, _ := s.st.IsBanned(r.Context(), viewer.ID); !banned {
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
	if page == 1 && len(pvm) > 0 {
		data.MetaDesc = metaDesc(pvm[0].ContentMD)
	}
	_ = s.rd.Render(w, "page_thread.html", &data)
}

// metaDesc 楼层 Markdown 转纯文本摘要（meta description / OG 用，≤150 字）。
func metaDesc(md string) string {
	var b strings.Builder
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>*- "))
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString(" ")
		if b.Len() > 200 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	r := []rune(out)
	if len(r) > 150 {
		out = string(r[:150]) + "…"
	}
	return out
}

// sitemap GET /sitemap.xml：首页 + 版块 + 最近公开主题（≤2000 条）。
func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	cats, err := s.st.CategoriesWithForums(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "生成失败", err.Error())
		return
	}
	threads, err := s.st.SitemapThreads(r.Context(), 2000)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "生成失败", err.Error())
		return
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`)
	loc := func(p string) string {
		return "  <url><loc>" + html.EscapeString(s.cfg.SiteURL+p) + "</loc></url>\n"
	}
	b.WriteString(loc("/"))
	for _, c := range cats {
		for _, f := range c.Forums {
			b.WriteString(loc(ForumURL(f.ID, 1)))
		}
	}
	for _, t := range threads {
		b.WriteString("  <url><loc>" + html.EscapeString(s.cfg.SiteURL+ThreadURL(t.ID, 1)) +
			"</loc><lastmod>" + t.LastPostAt.Format("2006-01-02") + "</lastmod></url>\n")
	}
	b.WriteString("</urlset>")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// rss GET /rss：全站最新主题 RSS 2.0。
func (s *Server) rss(w http.ResponseWriter, r *http.Request) {
	threads, _, err := s.st.LatestThreads(r.Context(), 1, 20)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "生成失败", err.Error())
		return
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
`)
	fmt.Fprintf(&b, "<title>%s</title>\n<link>%s</link>\n<description>%s 最新主题</description>\n",
		html.EscapeString(s.sets(r).SiteName), html.EscapeString(s.cfg.SiteURL), html.EscapeString(s.sets(r).SiteName))
	for _, it := range threads {
		link := s.cfg.SiteURL + ThreadURL(it.ID, 1)
		fmt.Fprintf(&b, "<item><title>%s</title><link>%s</link><guid>%s</guid><pubDate>%s</pubDate></item>\n",
			html.EscapeString(it.Title), html.EscapeString(link), html.EscapeString(link),
			it.LastPostAt.UTC().Format(time.RFC1123Z))
	}
	b.WriteString("</channel></rss>")
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// ---- 条款与隐私（ROADMAP 阶段六）----

func (s *Server) renderDoc(w http.ResponseWriter, r *http.Request, title, md string) {
	d := struct {
		Common
		HTML template.HTML
	}{s.common(r), toHTML(markdown.Render(md))}
	d.Title = title
	_ = s.rd.Render(w, "page_doc.html", &d)
}

func (s *Server) termsPage(w http.ResponseWriter, r *http.Request) {
	s.renderDoc(w, r, "服务条款", s.sets(r).TermsContent)
}

func (s *Server) privacyPage(w http.ResponseWriter, r *http.Request) {
	s.renderDoc(w, r, "隐私政策", s.sets(r).PrivacyContent)
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
