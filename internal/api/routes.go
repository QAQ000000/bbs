// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import "net/http"

// routes serves API and controlled media only. Nuxt owns all website URLs.
func (s *Server) routes() http.Handler {
	m := s.mux
	s.membershipRoutes()
	s.pointsRoutes()
	s.titleRoutes()
	m.HandleFunc("GET /api/v1/session", s.sessionGet)
	m.HandleFunc("GET /api/v1/site", s.siteGet)
	m.HandleFunc("GET /api/v1/home", s.homeGet)
	m.HandleFunc("GET /api/v1/forums", s.forumsGet)
	m.HandleFunc("GET /api/v1/forums/{fid}", s.forumGet)
	m.HandleFunc("GET /api/v1/threads", s.threadsGet)
	m.HandleFunc("GET /api/v1/threads/{tid}", s.threadGet)
	m.HandleFunc("GET /api/v1/threads/{tid}/posts", s.postsGet)
	m.HandleFunc("GET /api/v1/posts/{pid}", s.postGet)
	m.HandleFunc("GET /api/v1/search", s.searchGet)
	m.HandleFunc("GET /api/v1/users/{id}", s.userGet)
	m.HandleFunc("GET /api/v1/me", s.meGet)
	m.HandleFunc("GET /api/v1/me/sessions", s.sessionsGet)
	m.HandleFunc("GET /api/v1/me/2fa", s.mfaGet)
	m.HandleFunc("GET /api/v1/me/favorites", s.favoritesGet)
	m.HandleFunc("GET /api/v1/me/drafts", s.draftsGet)
	m.HandleFunc("GET /api/v1/me/draft", s.draftGet)
	m.HandleFunc("GET /api/v1/me/notifications", s.notificationsGet)
	m.HandleFunc("GET /api/v1/me/conversations", s.messagesGet)
	m.HandleFunc("GET /api/v1/me/subscriptions", s.subscriptionsGet)
	m.HandleFunc("GET /api/v1/me/following", s.followsGet)
	m.HandleFunc("GET /api/v1/me/followers", s.followsGet)
	m.HandleFunc("GET /api/v1/users/{id}/followers", s.followsGet)
	m.HandleFunc("GET /api/v1/tags", s.tagsGet)
	m.HandleFunc("GET /api/v1/tags/{tagId}", s.tagGet)
	m.HandleFunc("GET /api/v1/tag-slugs/{slug}", s.tagGet)
	m.HandleFunc("GET /api/v1/tags/{tagId}/threads", s.tagThreads)
	m.HandleFunc("GET /api/v1/conversations/{cid}/messages", s.conversationMessagesGet)
	m.HandleFunc("GET /api/v1/me/notifications/summary", s.notificationSummary)
	m.HandleFunc("GET /api/v1/me/notification-preferences", s.notificationPreferencesGet)
	m.HandleFunc("GET /api/v1/me/content", s.ownContentGet)
	m.HandleFunc("GET /api/v1/posts/{pid}/position", s.postPositionGet)
	m.HandleFunc("GET /api/v1/posts/{pid}/history", s.historyGet)
	m.HandleFunc("GET /api/v1/me/export", s.profileExport)
	m.HandleFunc("GET /api/v1/smileys", s.smileysGet)
	m.HandleFunc("GET /api/v1/setup", s.setupGet)
	m.HandleFunc("GET /api/v1/auth/captcha", s.captchaGet)
	m.HandleFunc("GET /api/v1/events", s.handleLive)
	m.HandleFunc("GET /api/v1/posts/{pid}/likes", s.likesList)
	for route, h := range map[string]http.HandlerFunc{
		"POST /api/v1/me/2fa/setup":              s.mfaSetup,
		"POST /api/v1/me/2fa/enable":             s.mfaEnable,
		"POST /api/v1/me/2fa/disable":            s.mfaDisable,
		"POST /api/v1/me/2fa/recovery-codes":     s.mfaRecovery,
		"POST /api/v1/auth/2fa":                  s.mfaLogin,
		"PATCH /api/v1/me/sessions/{sessionId}":  s.sessionManage,
		"DELETE /api/v1/me/sessions/{sessionId}": s.sessionManage,
		"POST /api/v1/me/sessions/revoke-others": s.sessionManage,
		"DELETE /api/v1/me/sessions":             s.sessionManage,
		"POST /api/v1/auth/login":                s.loginSubmit, "POST /api/v1/auth/register": s.registerSubmit, "POST /api/v1/auth/logout": s.logout,
		"POST /api/v1/auth/password/forgot": s.forgotSubmit, "POST /api/v1/auth/password/reset": s.resetSubmit, "POST /api/v1/auth/email/verify": s.verifyEmail,
		"POST /api/v1/me/email/verify-resend": s.profileVerifyResend, "POST /api/v1/setup": s.setupSubmit,
		"POST /api/v1/me/email/change":  s.profileEmailChange,
		"POST /api/v1/me/email/confirm": s.profileEmailConfirm,
		"PATCH /api/v1/me":              s.profileSave, "POST /api/v1/me/password": s.profilePassword, "POST /api/v1/me/avatar": s.profileAvatar,
		"DELETE /api/v1/me/avatar": s.profileAvatarClear, "DELETE /api/v1/me": s.profileSelfDelete,
		"POST /api/v1/threads": s.newThreadSubmit, "POST /api/v1/threads/{tid}/posts": s.replySubmit, "PATCH /api/v1/posts/{pid}": s.editSubmit, "DELETE /api/v1/posts/{pid}": s.deletePost,
		"POST /api/v1/posts/{pid}/like": s.likeToggle, "POST /api/v1/threads/{tid}/favorite": s.favoriteToggle,
		"POST /api/v1/me/draft": s.draftSave, "DELETE /api/v1/me/draft": s.draftDelete,
		"POST /api/v1/me/notifications/read": s.notificationsRead, "POST /api/v1/threads/{tid}/read": s.readRecord,
		"PUT /api/v1/me/notification-preferences": s.notificationPreferencesSave,
		"POST /api/v1/users/{id}/follow":          s.followToggle, "DELETE /api/v1/users/{id}/follow": s.followToggle,
		"POST /api/v1/threads/{tid}/subscribe": s.threadSubscribe, "DELETE /api/v1/threads/{tid}/subscribe": s.threadSubscribe,
		"POST /api/v1/users/{id}/messages":         s.messageSend,
		"POST /api/v1/conversations/{cid}/block":   s.conversationBlock,
		"DELETE /api/v1/conversations/{cid}/block": s.conversationBlock,
		"POST /api/v1/conversations/{cid}/read":    s.conversationRead,
		"PUT /api/v1/threads/{tid}/tags":           s.threadTagsSave,
		"PUT /api/v1/threads/{tid}/subscribe":      s.threadSubscribe,
		"POST /api/v1/forums/{fid}/subscribe":      s.threadSubscribe,
		"PUT /api/v1/forums/{fid}/subscribe":       s.threadSubscribe,
		"DELETE /api/v1/forums/{fid}/subscribe":    s.threadSubscribe,
		"POST /api/v1/tags/{tagId}/subscribe":      s.threadSubscribe,
		"PUT /api/v1/tags/{tagId}/subscribe":       s.threadSubscribe,
		"DELETE /api/v1/tags/{tagId}/subscribe":    s.threadSubscribe,
		"POST /api/v1/uploads":                     s.uploadImage, "POST /api/v1/posts/{pid}/reports": s.reportSubmit,
	} {
		m.HandleFunc(route, s.action(h))
	}
	for route, h := range map[string]http.HandlerFunc{
		"GET /api/v1/admin": s.adminDash, "GET /api/v1/admin/forums": s.adminForums, "GET /api/v1/admin/threads": s.adminThreads,
		"GET /api/v1/admin/forum-stats": s.adminForumStats,
		"GET /api/v1/admin/users":       s.adminUsers, "GET /api/v1/admin/settings": s.adminSettings, "GET /api/v1/admin/perms": s.adminPerms,
		"GET /api/v1/admin/logs": s.adminLogs, "GET /api/v1/admin/recyclebin": s.adminRecycle, "GET /api/v1/admin/censor": s.adminCensor,
		"GET /api/v1/admin/announcements": s.adminAnnounce, "GET /api/v1/admin/moderate": s.adminModerate,
		"GET /api/v1/admin/tags":            s.tagsGet,
		"GET /api/v1/admin/settings/schema": s.settingsSchema,
		"GET /api/v1/admin/settings/status": s.settingsStatus,
		"PUT /api/v1/admin/settings":        s.settingsJSONSave,
		"PATCH /api/v1/admin/settings":      s.settingsJSONSave,
		"GET /api/v1/admin/email-jobs":      s.adminEmailQueue,
	} {
		m.HandleFunc(route, s.adminPointGuard(route, h))
	}
	for route, h := range map[string]http.HandlerFunc{
		"POST /api/v1/admin/forums/save": s.adminForumSave, "POST /api/v1/admin/forums/delete": s.adminForumDelete, "POST /api/v1/admin/forums/move": s.adminForumMove,
		"POST /api/v1/admin/tags": s.tagSave, "PUT /api/v1/admin/tags/{tagId}": s.tagSave,
		"POST /api/v1/admin/email-jobs/{jobId}/retry": s.adminEmailRetry,
		"POST /api/v1/admin/cats/save":                s.adminCatSave, "POST /api/v1/admin/cats/delete": s.adminCatDelete, "POST /api/v1/admin/threads/action": s.adminThreadAction,
		"POST /api/v1/admin/users/ban": s.adminUserBan, "POST /api/v1/admin/users/unban": s.adminUserUnban, "POST /api/v1/admin/users/group": s.adminUserGroup,
		"POST /api/v1/admin/users/delete": s.adminUserDelete, "POST /api/v1/admin/users/block": s.adminUserBlock, "POST /api/v1/admin/users/unblock": s.adminUserUnblock,
		"POST /api/v1/admin/settings": s.adminSettingsSave, "POST /api/v1/admin/perms/save": s.adminPermsSave,
		"POST /api/v1/admin/recyclebin/restore": s.adminRecycleRestore, "POST /api/v1/admin/recyclebin/purge": s.adminRecyclePurge, "POST /api/v1/admin/recyclebin/purgeall": s.adminRecyclePurgeAll,
		"POST /api/v1/admin/censor/add": s.adminCensorAdd, "POST /api/v1/admin/censor/delete": s.adminCensorDelete,
		"POST /api/v1/admin/moderate/thread": s.adminModerateThread, "POST /api/v1/admin/moderate/post": s.adminModeratePost, "POST /api/v1/admin/report/handle": s.adminReportHandle,
		"POST /api/v1/admin/prune/execute": s.adminPruneExecute, "POST /api/v1/admin/announcements/add": s.adminAnnounceAdd,
		"POST /api/v1/admin/announcements/toggle": s.adminAnnounceToggle, "POST /api/v1/admin/announcements/delete": s.adminAnnounceDelete,
	} {
		m.HandleFunc(route, s.adminPointGuard(route, s.action(h)))
	}
	// Existing media and monitoring URLs remain valid in stored posts and tooling.
	m.HandleFunc("GET /uploads/", s.serveUploads)
	m.HandleFunc("GET /smiley/{pkg}/{file}", s.serveSmiley)
	m.HandleFunc("GET /avatar/{uid}", s.avatarSVG)
	m.HandleFunc("GET /captcha/{id}", s.captchaImage)
	m.HandleFunc("GET /api/status", s.handleStatus)
	m.HandleFunc("GET /api/v1/health/ready", s.handleStatus)
	m.HandleFunc("GET /api/v1/health/live", func(w http.ResponseWriter, r *http.Request) { s.respond(w, 200, map[string]bool{"ok": true}) })
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, 404, "NOT_FOUND", "接口不存在") })
	s.handler = chain(m, s.recoverMW, s.logMW, s.securityMW, s.gzipMW, s.timeoutMW, s.authMW, s.apiStateMW, s.membershipMW)
	return s.handler
}
