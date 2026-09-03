// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"dzforum/internal/store"
)

// ---- 后台公共 ----

// requireAdmin 后台访问守卫：需登录且为管理员。
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	u := User(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+urlQueryEscape(r.URL.RequestURI()), http.StatusFound)
		return false
	}
	if !u.IsAdmin() {
		s.renderError(w, r, http.StatusForbidden, "无权访问", "该区域仅管理员可访问。")
		return false
	}
	return true
}

func (s *Server) sets(r *http.Request) store.SiteSettings {
	return s.st.Settings(r.Context())
}

// adminCommon 后台页面公共数据。
func (s *Server) adminCommon(r *http.Request, active string) Common {
	c := s.common(r)
	c.NavActive = active
	return c
}

func adminPageItems(cur, total int) []PageItem {
	return BuildPage(cur, total, func(n int) string { return "/admin/" + strconv.Itoa(n) })
}

var _ = adminPageItems

// ---- 仪表盘 ----

func (s *Server) adminDash(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	st, err := s.st.SiteStats(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	data := struct {
		Common
		Stats      store.SiteStats
		Recycle    int64
		Banned     int64
		DBSize     string
		Subs       int
		Uptime     string
		GoVersion  string
	}{s.adminCommon(r, "dash"), st,
		s.st.RecycleCount(r.Context()), s.st.BannedCount(r.Context()),
		s.st.DBSize(r.Context()), s.hub.Count(),
		time.Since(s.start).Round(time.Second).String(), runtime.Version()}
	_ = s.rd.Render(w, "admin_dash.html", &data)
}

// ---- 版块管理 ----

func (s *Server) adminForums(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	cats, err := s.st.CategoriesWithForums(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	data := struct {
		Common
		Categories []*store.Category
	}{s.adminCommon(r, "forums"), cats}
	_ = s.rd.Render(w, "admin_forums.html", &data)
}

// ---- 内容管理 ----

func (s *Server) adminThreads(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	fid, _ := strconv.ParseInt(r.URL.Query().Get("forum"), 10, 64)
	q := store.ThreadQuery{
		ForumID:        fid,
		Keyword:        strings.TrimSpace(r.URL.Query().Get("q")),
		Author:         strings.TrimSpace(r.URL.Query().Get("author")),
		Page:           page,
		Size:           20,
		OnlyForumIDs:   s.staffForumScope(r),
		IncludePending: true,
	}
	threads, total, err := s.st.SearchThreads(r.Context(), q)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	cats, _ := s.st.CategoriesWithForums(r.Context())
	totalPage := (total + q.Size - 1) / q.Size
	if totalPage < 1 {
		totalPage = 1
	}
	data := struct {
		Common
		Threads    []*store.Thread
		Categories []*store.Category
		ForumID    int64
		Keyword    string
		Author     string
		BackURL    string
		Page       []PageItem
		PageNum    int
		TotalPage  int
	}{s.adminCommon(r, "threads"), threads, cats, fid, q.Keyword, q.Author,
		adminThreadSearchURL(fid, q.Keyword, q.Author, page), nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string {
		return adminThreadSearchURL(fid, q.Keyword, q.Author, n)
	})
	_ = s.rd.Render(w, "admin_threads.html", &data)
}

func adminThreadSearchURL(fid int64, kw, author string, page int) string {
	v := url.Values{}
	if fid > 0 {
		v.Set("forum", strconv.FormatInt(fid, 10))
	}
	if kw != "" {
		v.Set("q", kw)
	}
	if author != "" {
		v.Set("author", author)
	}
	v.Set("page", strconv.Itoa(page))
	return "/admin/threads?" + v.Encode()
}

// ---- 用户管理 ----

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	kw := strings.TrimSpace(r.URL.Query().Get("q"))
	users, total, err := s.st.SearchUsers(r.Context(), store.UserQuery{Keyword: kw, Page: page, Size: 20})
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	totalPage := (total + 19) / 20
	if totalPage < 1 {
		totalPage = 1
	}
	viewer := User(r)
	data := struct {
		Common
		Users     []*store.AdminUser
		Keyword   string
		ViewerID  int64
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.adminCommon(r, "users"), users, kw, viewer.ID, nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string {
		u := "/admin/users?"
		if kw != "" {
			v := url.Values{}
			v.Set("q", kw)
			u += v.Encode() + "&"
		}
		return u + "page=" + strconv.Itoa(n)
	})
	_ = s.rd.Render(w, "admin_users.html", &data)
}

// ---- 站点设置 ----

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	st := s.sets(r)
	data := struct {
		Common
		Settings store.SiteSettings
		Saved    bool
	}{s.adminCommon(r, "settings"), st, r.URL.Query().Get("saved") == "1"}
	_ = s.rd.Render(w, "admin_settings.html", &data)
}

// ---- 日志 ----

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	logs, total, err := s.st.AdminLogs(r.Context(), page, 30)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	totalPage := (total + 29) / 30
	if totalPage < 1 {
		totalPage = 1
	}
	data := struct {
		Common
		Logs      []*store.AdminLogEntry
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.adminCommon(r, "logs"), logs, nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string {
		return "/admin/logs?page=" + strconv.Itoa(n)
	})
	_ = s.rd.Render(w, "admin_logs.html", &data)
}
