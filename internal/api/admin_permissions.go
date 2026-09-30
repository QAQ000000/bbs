// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"dzforum/internal/perm"
	"dzforum/internal/store"
	"net/http"
	"strings"
)

func requestWithUser(r *http.Request, u *store.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxUser, u))
}
func (s *Server) adminPointGuard(route string, h http.HandlerFunc) http.HandlerFunc {
	var point perm.Point
	switch {
	case route == "PUT /api/v1/admin/engagement/config":
		point = perm.EngagementConfigure
	case strings.Contains(route, "/engagement/"):
		point = perm.EngagementView
	case strings.Contains(route, "/admin/polls"):
		point = perm.PollManage
	case strings.Contains(route, "/admin/bounties"):
		point = perm.BountyManage
	case route == "PUT /api/v1/admin/points/config":
		point = perm.PointsConfigure
	case strings.Contains(route, "/points/") && strings.HasSuffix(route, "/adjust"):
		point = perm.PointsAdjust
	case strings.Contains(route, "/points/"):
		point = perm.PointsView
	case strings.Contains(route, "/email-jobs"):
		point = perm.EmailManage
	case strings.Contains(route, "/sessions"):
		point = perm.SessionsManage
	case strings.Contains(route, "/tags"):
		point = perm.TagsConfigure
	case strings.Contains(route, "/titles/") && strings.HasPrefix(route, "PATCH "):
		point = perm.TitleView
	case strings.Contains(route, "/titles/") && strings.HasSuffix(route, "/logs"):
		point = perm.TitleLogs
	case strings.Contains(route, "/titles") && (strings.HasPrefix(route, "POST ") || strings.HasPrefix(route, "PUT ")):
		point = perm.TitleConfigure
	case strings.Contains(route, "/titles"):
		point = perm.TitleView
	case strings.Contains(route, "/membership/users/") && strings.HasPrefix(route, "PATCH "):
		point = perm.MemberView // body selects adjust permissions separately
	case strings.Contains(route, "/membership/logs"):
		point = perm.MemberLogs
	case strings.Contains(route, "/membership") && (strings.HasPrefix(route, "PUT ") || strings.HasPrefix(route, "POST ")):
		point = perm.MemberConfigure
	case strings.Contains(route, "/membership"):
		point = perm.MemberView
	case strings.Contains(route, "/perms"):
		point = perm.PermissionsEdit
	case strings.Contains(route, "/settings"):
		point = perm.SettingsEdit
	case strings.Contains(route, "/censor"):
		point = perm.CensorManage
	case strings.Contains(route, "/announcements"):
		point = perm.AnnounceManage
	case strings.Contains(route, "/logs"):
		point = perm.LogsView
	case strings.Contains(route, "/forums") || strings.Contains(route, "/cats"):
		point = perm.ForumManage
	case strings.Contains(route, "/users/block") || strings.Contains(route, "/users/unblock"):
		point = perm.UserBan
	case route == "GET /api/v1/admin/users":
		point = perm.UsersView
	}
	if point == "" {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAdmin(w, r) {
			return
		}
		if !hasPoint(User(r), point) {
			s.fail(w, r, 403, "FORBIDDEN", "缺少操作权限："+string(point))
			return
		}
		h(w, r)
	}
}
