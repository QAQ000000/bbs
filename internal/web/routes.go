// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"net/http"
	"strconv"
	"strings"
)

// routes 注册全部路由。
func (s *Server) routes() http.Handler {
	m := s.mux
	m.HandleFunc("GET /{$}", s.cachedPage(s.handleHome))
	m.HandleFunc("GET /latest", s.cachedPage(s.latestPage))
	m.HandleFunc("GET /search", s.cachedPage(s.searchPage))

	// 静态与杂项
	m.HandleFunc("GET /static/", s.handleStatic)
	m.HandleFunc("GET /favicon.ico", s.handleFavicon)
	m.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	})

	// 实时
	m.HandleFunc("GET /api/live", s.handleLive)
	m.HandleFunc("POST /api/preview", s.handlePreview)
	m.HandleFunc("POST /api/like/{pid}", s.likeToggle)
	m.HandleFunc("GET /api/draft", s.draftGet)
	m.HandleFunc("POST /api/upload", s.uploadImage)
	m.HandleFunc("GET /uploads/", s.serveUploads)
	m.HandleFunc("GET /smiley/{pkg}/{file}", s.serveSmiley)
	m.HandleFunc("GET /avatar/{uid}", s.avatarSVG)
	m.HandleFunc("GET /api/likes/{pid}", s.likesList)
	m.HandleFunc("GET /notify", s.notifyPage)
	m.HandleFunc("POST /api/draft", s.draftSave)
	m.HandleFunc("GET /api/status", s.handleStatus)

	// 用户
	m.HandleFunc("GET /forgot", s.forgotForm)
	m.HandleFunc("POST /forgot", s.forgotSubmit)
	m.HandleFunc("GET /reset", s.resetForm)
	m.HandleFunc("POST /reset", s.resetSubmit)
	m.HandleFunc("GET /login", s.loginForm)
	m.HandleFunc("POST /login", s.loginSubmit)
	m.HandleFunc("GET /register", s.registerForm)
	m.HandleFunc("POST /register", s.registerSubmit)
	m.HandleFunc("POST /logout", s.logout)
	m.HandleFunc("GET /user/{id}", s.userPage)
	m.HandleFunc("GET /profile", s.profileForm)
	m.HandleFunc("POST /profile/save", s.profileSave)
	m.HandleFunc("POST /profile/password", s.profilePassword)
	m.HandleFunc("POST /report/{pid}", s.reportSubmit)

	// 发帖
	m.HandleFunc("GET /new", s.newThreadForm)
	m.HandleFunc("POST /new", s.newThreadSubmit)
	m.HandleFunc("GET /reply/{tid}", s.replyForm)
	m.HandleFunc("POST /reply/{tid}", s.replySubmit)
	m.HandleFunc("GET /edit/{pid}", s.editForm)
	m.HandleFunc("POST /edit/{pid}", s.editSubmit)
	m.HandleFunc("POST /delete/{pid}", s.deletePost)

	// 后台
	m.HandleFunc("GET /admin", s.adminDash)
	m.HandleFunc("GET /admin/{$}", s.adminDash)
	m.HandleFunc("GET /admin/forums", s.adminForums)
	m.HandleFunc("POST /admin/forums/save", s.adminForumSave)
	m.HandleFunc("POST /admin/forums/delete", s.adminForumDelete)
	m.HandleFunc("POST /admin/forums/move", s.adminForumMove)
	m.HandleFunc("POST /admin/cats/save", s.adminCatSave)
	m.HandleFunc("POST /admin/cats/delete", s.adminCatDelete)
	m.HandleFunc("GET /admin/threads", s.adminThreads)
	m.HandleFunc("POST /admin/threads/action", s.adminThreadAction)
	m.HandleFunc("GET /admin/users", s.adminUsers)
	m.HandleFunc("POST /admin/users/ban", s.adminUserBan)
	m.HandleFunc("POST /admin/users/unban", s.adminUserUnban)
	m.HandleFunc("POST /admin/users/group", s.adminUserGroup)
	m.HandleFunc("POST /admin/users/delete", s.adminUserDelete)
	m.HandleFunc("GET /admin/settings", s.adminSettings)
	m.HandleFunc("POST /admin/settings", s.adminSettingsSave)
	m.HandleFunc("GET /admin/logs", s.adminLogs)
	m.HandleFunc("GET /admin/recyclebin", s.adminRecycle)
	m.HandleFunc("POST /admin/recyclebin/restore", s.adminRecycleRestore)
	m.HandleFunc("POST /admin/recyclebin/purge", s.adminRecyclePurge)
	m.HandleFunc("POST /admin/recyclebin/purgeall", s.adminRecyclePurgeAll)
	m.HandleFunc("GET /admin/censor", s.adminCensor)
	m.HandleFunc("POST /admin/censor/add", s.adminCensorAdd)
	m.HandleFunc("POST /admin/censor/delete", s.adminCensorDelete)
	m.HandleFunc("GET /admin/announcements", s.adminAnnounce)
	m.HandleFunc("GET /admin/moderate", s.adminModerate)
	m.HandleFunc("POST /admin/moderate/thread", s.adminModerateThread)
	m.HandleFunc("POST /admin/moderate/post", s.adminModeratePost)
	m.HandleFunc("POST /admin/report/handle", s.adminReportHandle)
	m.HandleFunc("GET /admin/prune", s.adminPrune)
	m.HandleFunc("POST /admin/prune/execute", s.adminPruneExecute)
	m.HandleFunc("POST /admin/announcements/add", s.adminAnnounceAdd)
	m.HandleFunc("POST /admin/announcements/toggle", s.adminAnnounceToggle)
	m.HandleFunc("POST /admin/announcements/delete", s.adminAnnounceDelete)

	// 伪静态（forum-N-P.html / thread-N-P-L.html）与兜底 404
	m.HandleFunc("GET /", s.handlePseudostatic)

	s.handler = chain(m,
		s.recoverMW,
		s.logMW,
		s.securityMW,
		s.gzipMW,
		s.authMW,
		s.siteOpenMW,
	)
	return s.handler
}

// handlePseudostatic 兜底路由：解析伪静态地址，其余 404。
func (s *Server) handlePseudostatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.NotFound(w, r)
		return
	}
	t, ok := parsePseudostatic(r.URL.Path)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "页面不存在", "您访问的地址不存在或已被删除。")
		return
	}
	if t.kind == "thread" && t.prev != 1 {
		http.Redirect(w, r, ThreadURL(t.id, t.page), http.StatusMovedPermanently)
		return
	}
	switch t.kind {
	case "forum":
		s.cachedPage(func(w http.ResponseWriter, r *http.Request) {
			s.handleForum(w, r, t.id, clampPage(int(t.page)))
		})(w, r)
	case "thread":
		s.cachedPage(func(w http.ResponseWriter, r *http.Request) {
			s.handleThread(w, r, t.id, clampPage(int(t.page)))
		})(w, r)
	}
}

func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// renderError 统一错误页：后台路径使用后台布局，其余用前台布局。
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, title, msg string) {
	w.WriteHeader(code)
	data := struct {
		Common
		Title string
		Msg   string
	}{s.common(r), title, msg}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		_ = s.rd.Render(w, "admin_error.html", &data)
		return
	}
	_ = s.rd.Render(w, "page_error.html", &data)
}
