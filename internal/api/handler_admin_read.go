// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"dzforum/internal/perm"
	"dzforum/internal/store"
	"net/http"
	"runtime"
	"strings"
	"time"
)

func (s *Server) adminAnalyticsSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" || len(name) > 64 {
		s.respond(w, 422, map[string]any{"error": "invalid snapshot name"})
		return
	}
	raw, generated, err := s.st.LatestAnalyticsSnapshot(r.Context(), name)
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"name": name, "generatedAt": generated, "payload": raw})
}

func (s *Server) adminDash(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v, err := s.st.SiteStats(r.Context())
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"stats": statsDTO(v), "recycle": s.st.RecycleCount(r.Context()), "banned": s.st.BannedCount(r.Context()), "dbSize": s.st.DBSize(r.Context()), "databasePool": s.st.DatabasePoolStats(), "subscriptions": s.hub.Count(), "uptimeSeconds": int64(time.Since(s.start).Seconds()), "goVersion": runtime.Version(), "uploadBytes": s.uploadDirBytes(), "uploadLimitGB": s.sets(r).UploadMaxDiskGB})
}

func (s *Server) adminForumStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v, err := s.st.ForumStatsQueueStatus(r.Context())
	if !s.readError(w, r, err) {
		s.respond(w, 200, v)
	}
}

func (s *Server) adminSearchStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v, err := s.st.SearchIndexQueueStatus(r.Context())
	if !s.readError(w, r, err) {
		s.respond(w, 200, v)
	}
}

func (s *Server) adminDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	locks, err := s.st.DatabaseLockWaits(r.Context())
	if s.readError(w, r, err) {
		return
	}
	workload, err := s.st.DatabaseWorkloadStats(r.Context())
	if s.readError(w, r, err) {
		return
	}
	forum, err := s.st.ForumStatsQueueStatus(r.Context())
	if s.readError(w, r, err) {
		return
	}
	search, err := s.st.SearchIndexQueueStatus(r.Context())
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"databasePool": s.st.DatabasePoolStats(), "databaseWorkload": workload, "lockWaits": locks, "forumStats": forum, "searchIndex": search})
}
func (s *Server) adminForums(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	s.forumsGet(w, r)
}
func (s *Server) adminThreads(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.SearchThreads(r.Context(), store.ThreadQuery{ForumID: queryID(r, "forum"), Keyword: r.URL.Query().Get("q"), Author: r.URL.Query().Get("author"), Page: page, Size: 20, OnlyForumIDs: s.staffForumScope(r), IncludePending: true})
	if !s.readError(w, r, err) {
		s.list(w, mapRows(rows, threadDTO), page, 20, total)
	}
}
func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.SearchUsers(r.Context(), store.UserQuery{Keyword: r.URL.Query().Get("q"), Page: page, Size: 20})
	if s.readError(w, r, err) {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, u := range rows {
		ids = append(ids, u.ID)
	}
	levels, err := s.st.MemberSummaries(r.Context(), ids)
	if s.readError(w, r, err) {
		return
	}
	s.list(w, mapRows(rows, func(u *store.AdminUser) map[string]any {
		v := adminUserDTO(u)
		v["level"] = levels[u.ID]
		return v
	}), page, 20, total)
}
func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v, err := s.st.Settings(r.Context())
	if !s.settingsError(w, r, err, true) {
		s.respond(w, 200, v)
	}
}
func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.AdminLogs(r.Context(), page, 30)
	if !s.readError(w, r, err) {
		s.list(w, mapRows(rows, adminLogDTO), page, 30, total)
	}
}
func (s *Server) adminRecycle(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaffPoint(w, r, perm.RecycleBin) {
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.RecycleThreads(r.Context(), page, 20, s.staffForumScope(r))
	if !s.readError(w, r, err) {
		s.list(w, mapRows(rows, threadDTO), page, 20, total)
	}
}
func (s *Server) adminCensor(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	s.respond(w, 200, mapRows(s.st.CensorWords(r.Context()), func(v store.CensorWord) map[string]any {
		return map[string]any{"id": idString(v.ID), "word": v.Word, "replacement": v.Replacement}
	}))
}
func (s *Server) adminAnnounce(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v, err := s.st.Announcements(r.Context(), false, 50)
	if !s.readError(w, r, err) {
		s.respond(w, 200, mapRows(v, announcementDTO))
	}
}
func (s *Server) adminPerms(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	s.respond(w, 200, map[string]any{"points": perm.AllPoints(), "matrix": perm.Matrix()})
}
func (s *Server) adminModerate(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaffPoint(w, r, perm.ModerateQueue) {
		return
	}
	scope := s.staffForumScope(r)
	th, err := s.st.PendingThreads(r.Context(), 50, scope)
	if s.readError(w, r, err) {
		return
	}
	posts, err := s.st.PendingPosts(r.Context(), 50, scope)
	if s.readError(w, r, err) {
		return
	}
	reports, err := s.st.OpenReports(r.Context(), 50, scope)
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"threads": mapRows(th, func(v *store.PendingThreadRow) map[string]any {
		m := threadDTO(&v.Thread)
		m["excerpt"] = v.Excerpt
		return m
	}), "posts": mapRows(posts, func(v *store.PendingPostRow) map[string]any {
		m := postDTO(&v.Post)
		m["threadTitle"] = v.ThreadTtl
		return m
	}), "reports": mapRows(reports, reportDTO)})
}
