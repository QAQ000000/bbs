// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"dzforum/internal/perm"
	"dzforum/internal/store"
	"net"
	"net/http"
)

// Explicit response projections: never expose a store model wholesale.
func publicUser(u *store.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{"id": idString(u.ID), "username": u.Username, "groupId": u.GroupID, "postCount": u.PostCount, "signature": u.Signature, "createdAt": u.CreatedAt, "avatarUrl": "/avatar/" + idString(u.ID)}
}
func privateUser(u *store.User) map[string]any {
	if u == nil {
		return nil
	}
	m := publicUser(u)
	m["email"] = u.Email
	m["emailVerified"] = u.EmailVerified
	m["mustChangePassword"] = u.MustChangePassword
	return m
}
func threadDTO(t *store.Thread) map[string]any {
	return map[string]any{"id": idString(t.ID), "forumId": idString(t.ForumID), "authorId": idString(t.AuthorID), "authorName": t.AuthorName, "title": t.Title, "sticky": t.Sticky, "digest": t.Digest, "closed": t.Closed, "postCount": t.PostCount, "viewCount": t.ViewCount, "createdAt": t.CreatedAt, "lastPostAt": t.LastPostAt, "lastPostUserId": idString(t.LastPostUID), "lastPostName": t.LastPostName, "firstPostId": idString(t.FirstPostID), "pending": t.Pending, "pendingReason": t.PendingReason}
}
func forumDTO(f *store.Forum) map[string]any {
	return map[string]any{"id": idString(f.ID), "categoryId": idString(int64(f.CategoryID)), "name": f.Name, "description": f.Description, "threadCount": f.ThreadCount, "postCount": f.PostCount, "todayCount": f.TodayCount, "lastPostAt": f.LastPostAt, "lastThreadId": idString(f.LastThreadID), "lastThreadTitle": f.LastThreadTitle, "lastPostAuthor": f.LastPostAuthor, "moderators": f.Moderators}
}
func categoriesDTO(cats []*store.Category) []map[string]any {
	out := make([]map[string]any, 0, len(cats))
	for _, c := range cats {
		out = append(out, map[string]any{"id": idString(int64(c.ID)), "name": c.Name, "forums": mapRows(c.Forums, forumDTO)})
	}
	return out
}
func postDTO(p *store.Post) map[string]any {
	return map[string]any{"id": idString(p.ID), "threadId": idString(p.ThreadID), "authorId": idString(p.AuthorID), "authorName": p.AuthorName, "authorGroup": p.AuthorGroup, "floor": p.Floor, "content": p.ContentMD, "createdAt": p.CreatedAt, "editedAt": p.EditedAt, "hasEdited": p.HasEdited, "pending": p.Pending, "pendingReason": p.PendingReason, "likeCount": p.LikeCount, "version": p.Version}
}
func (s *Server) postResponse(r *http.Request, p *store.Post, th *store.Thread) map[string]any {
	m := postDTO(p)
	u := User(r)
	m["capabilities"] = map[string]bool{
		"canEdit":     s.memberDecision(r, "post.edit", th.ForumID, p, th).Allowed,
		"canDelete":   s.memberDecision(r, "post.delete", th.ForumID, p, th).Allowed,
		"canLike":     s.memberDecision(r, "post.like", th.ForumID, p, th).Allowed,
		"canReport":   s.memberDecision(r, "post.report", th.ForumID, p, th).Allowed,
		"canModerate": s.canModerateThread(r, th),
	}
	if hasPoint(u, perm.AdminPanel) && p.IP != "" {
		m["maskedIp"] = maskIP(p.IP)
	}
	return m
}
func maskIP(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return "***"
	}
	if v := ip.To4(); v != nil {
		return net.IPv4(v[0], v[1], v[2], 0).String() + "/24"
	}
	v := ip.To16()
	for i := 6; i < len(v); i++ {
		v[i] = 0
	}
	return v.String() + "/48"
}
func uploadDTO(u store.Upload) map[string]any {
	return map[string]any{"id": idString(u.ID), "postId": idString(u.PostID), "name": u.Name, "url": u.Path, "size": u.Size, "mime": u.Mime}
}
func statsDTO(v store.SiteStats) map[string]any {
	return map[string]any{"todayPosts": v.TodayPosts, "yesterdayPosts": v.Yesterday, "totalPosts": v.TotalPosts, "totalThreads": v.TotalThreads, "members": v.Members}
}
func announcementDTO(v *store.Announcement) map[string]any {
	return map[string]any{"id": idString(v.ID), "authorId": idString(v.UID), "author": v.Author, "content": v.Content, "enabled": v.Enabled, "createdAt": v.CreatedAt}
}
func mapRows[T any](rows []T, convert func(T) map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert(r))
	}
	return out
}
func (s *Server) list(w http.ResponseWriter, data any, page, size, total int) {
	writeJSON(w, 200, map[string]any{"data": data, "meta": map[string]int{"page": page, "pageSize": size, "total": total, "totalPages": (total + size - 1) / size}})
}

func adminUserDTO(v *store.AdminUser) map[string]any {
	return map[string]any{"id": idString(v.ID), "username": v.Username, "email": v.Email, "groupId": v.GroupID, "postCount": v.PostCount, "createdAt": v.CreatedAt, "lastLoginAt": v.LastLoginAt, "bannedUntil": v.BannedUntil, "banReason": v.BanReason, "isBanned": v.IsBanned, "blockedUntil": v.BlockedUntil, "isBlocked": v.IsBlocked}
}

func adminLogDTO(v *store.AdminLogEntry) map[string]any {
	return map[string]any{"id": idString(v.ID), "uId": idString(v.UID), "username": v.Username, "action": v.Action, "detail": v.Detail, "maskedIp": maskIP(v.IP), "createdAt": v.CreatedAt}
}

func settingsDTO(v *store.SiteSettings) map[string]any {
	return map[string]any{"siteName": v.SiteName, "threadsPerPage": v.ThreadsPerPage, "postsPerPage": v.PostsPerPage, "registerEnabled": v.RegisterEnabled, "siteClosed": v.SiteClosed, "siteClosedReason": v.SiteClosedReason, "moderateEnabled": v.ModerateEnabled, "uploadEnabled": v.UploadEnabled, "maxImageMB": v.MaxImageMB, "maxFileMB": v.MaxFileMB, "uploadMaxDiskGB": v.UploadMaxDiskGB, "captchaEnabled": v.CaptchaEnabled, "emailVerifyEnabled": v.EmailVerifyEnabled, "requireConsent": v.RequireConsent, "termsContent": v.TermsContent, "privacyContent": v.PrivacyContent, "siteLogo": v.SiteLogo, "footerText": v.FooterText}
}

func reportDTO(v *store.ReportRow) map[string]any {
	return map[string]any{"id": idString(v.ID), "postId": idString(v.PostID), "reporterId": idString(v.ReporterID), "reporter": v.Reporter, "reason": v.Reason, "createdAt": v.CreatedAt, "excerpt": v.Excerpt, "floor": v.Floor, "pending": v.Pending, "deleted": v.Deleted, "tId": idString(v.TID), "threadTtl": v.ThreadTtl, "authorName": v.AuthorName}
}
