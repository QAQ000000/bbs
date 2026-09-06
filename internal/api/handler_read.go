// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"dzforum/internal/captcha"

	"dzforum/internal/smiley"
	"dzforum/internal/store"
	"errors"

	"net/http"
	"strconv"

	"time"
)

func queryID(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return v
}
func pageOf(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return clampPage(n)
}
func (s *Server) readError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, 404, "NOT_FOUND", "内容不存在")
	} else {
		s.fail(w, r, 500, "INTERNAL_ERROR", err.Error())
	}
	return true
}
func (s *Server) sessionGet(w http.ResponseWriter, r *http.Request) {
	token := ""
	if sess := Session(r); sess != nil {
		token = sess.CSRF
	} else {
		token = s.anonCSRF(r, w)
	}
	u, err := s.memberUser(r, User(r), true)
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"user": u, "csrfToken": token, "setupRequired": s.setupRequired()})
}
func (s *Server) siteGet(w http.ResponseWriter, r *http.Request) {
	v := s.sets(r)
	logo := v.SiteLogo
	if logo == "" {
		logo = s.cfg.SiteLogo
	}
	s.respond(w, 200, map[string]any{"name": v.SiteName, "logo": logo, "footerText": v.FooterText, "threadsPerPage": v.ThreadsPerPage, "postsPerPage": v.PostsPerPage, "registerEnabled": v.RegisterEnabled, "siteClosed": v.SiteClosed, "siteClosedReason": v.SiteClosedReason, "uploadEnabled": v.UploadEnabled, "maxImageMB": v.MaxImageMB, "maxFileMB": v.MaxFileMB, "captchaEnabled": v.CaptchaEnabled, "requireConsent": v.RequireConsent, "emailVerificationRequired": s.emailGateEnabled(), "termsContent": v.TermsContent, "privacyContent": v.PrivacyContent})
}
func (s *Server) homeGet(w http.ResponseWriter, r *http.Request) {
	cats, err := s.st.CategoriesWithForums(r.Context())
	if s.readError(w, r, err) {
		return
	}
	stats, err := s.st.SiteStats(r.Context())
	if s.readError(w, r, err) {
		return
	}
	rows, _, err := s.st.LatestThreads(r.Context(), 1, 10)
	if s.readError(w, r, err) {
		return
	}
	ann, err := s.st.Announcements(r.Context(), true, 3)
	if s.readError(w, r, err) {
		return
	}
	threadRows, err := s.memberThreadRows(r, rows)
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"categories": s.memberCategoriesResponse(r, cats), "stats": statsDTO(stats), "threads": threadRows, "announcements": mapRows(ann, announcementDTO)})
}
func (s *Server) forumsGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.CategoriesWithForums(r.Context())
	if !s.readError(w, r, err) {
		s.respond(w, 200, s.memberCategoriesResponse(r, v))
	}
}
func (s *Server) forumGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.Forum(r.Context(), pathID(r, "fid"))
	if !s.readError(w, r, err) {
		s.respond(w, 200, s.memberForumResponse(r, v))
	}
}
func (s *Server) threadsGet(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	size := s.sets(r).ThreadsPerPage
	fid := queryID(r, "forumId")
	var rows []*store.Thread
	var total int
	var err error
	if fid > 0 {
		if _, err = s.st.Forum(r.Context(), fid); s.readError(w, r, err) {
			return
		}
		rows, total, err = s.st.Threads(r.Context(), fid, page, size, r.URL.Query().Get("sort"))
	} else {
		rows, total, err = s.st.LatestThreads(r.Context(), page, size)
	}
	if s.readError(w, r, err) {
		return
	}
	threadRows, err := s.memberThreadRows(r, rows)
	if s.readError(w, r, err) {
		return
	}
	data := map[string]any{"threads": threadRows, "stickies": []map[string]any{}}
	if fid > 0 {
		sticky, e := s.st.Stickies(r.Context(), fid)
		if s.readError(w, r, e) {
			return
		}
		data["stickies"], e = s.memberThreadRows(r, sticky)
		if s.readError(w, r, e) {
			return
		}
	}
	s.list(w, data, page, size, total)
}
func (s *Server) visibleThread(w http.ResponseWriter, r *http.Request, tid int64) *store.Thread {
	th, err := s.st.Thread(r.Context(), tid)
	if s.readError(w, r, err) {
		return nil
	}
	if !s.canViewThread(r, th) {
		s.fail(w, r, 404, "NOT_FOUND", "内容不存在")
		return nil
	}
	return th
}
func (s *Server) canViewThread(r *http.Request, th *store.Thread) bool {
	return canReadForum(r, th.ForumID) && (!th.Pending || User(r) != nil && (User(r).ID == th.AuthorID || s.canModerateThread(r, th)))
}
func (s *Server) canViewPost(r *http.Request, p *store.Post, th *store.Thread) bool {
	return s.canViewThread(r, th) && (!p.Pending || User(r) != nil && (User(r).ID == p.AuthorID || s.canModerateThread(r, th)))
}
func (s *Server) threadGet(w http.ResponseWriter, r *http.Request) {
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	m := threadDTO(th)
	badges, err := s.st.MemberSummaries(r.Context(), []int64{th.AuthorID})
	if s.readError(w, r, err) {
		return
	}
	m["authorLevel"] = badges[th.AuthorID]
	titles, err := s.st.EquippedTitles(r.Context(), []int64{th.AuthorID})
	if s.readError(w, r, err) {
		return
	}
	accepted, err := s.st.AcceptedReply(r.Context(), th.ID)
	if s.readError(w, r, err) {
		return
	}
	m["equippedTitle"] = titles[th.AuthorID]
	m["acceptedPostId"] = idString(accepted)
	m["capabilities"] = map[string]bool{"canModerate": s.canModerateThread(r, th), "canReply": s.memberDecision(r, "post.reply", th.ForumID, nil, th).Allowed}
	if User(r) != nil {
		m["favorite"] = s.st.IsFavorite(r.Context(), User(r).ID, th.ID)
	}
	s.respond(w, 200, m)
}
func (s *Server) postsGet(w http.ResponseWriter, r *http.Request) {
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	page, size := pageOf(r), s.sets(r).PostsPerPage
	uid := int64(0)
	if User(r) != nil {
		uid = User(r).ID
	}
	rows, total, err := s.st.VisiblePosts(r.Context(), th.ID, uid, s.canModerateThread(r, th), page, size)
	if s.readError(w, r, err) {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.ID)
	}
	atts, err := s.st.UploadsForPosts(r.Context(), ids)
	if s.readError(w, r, err) {
		return
	}
	states, err := s.st.PostViewerStates(r.Context(), ids, uid)
	if s.readError(w, r, err) {
		return
	}
	authorIDs := []int64{}
	for _, p := range rows {
		authorIDs = append(authorIDs, p.AuthorID)
	}
	badges, err := s.st.MemberSummaries(r.Context(), authorIDs)
	if s.readError(w, r, err) {
		return
	}
	out := mapRows(rows, func(p *store.Post) map[string]any {
		m := s.postResponse(r, p, th)
		m["authorLevel"] = badges[p.AuthorID]
		m["attachments"] = mapRows(atts[p.ID], uploadDTO)
		m["viewerHasLiked"] = states[p.ID].Liked
		m["replyTo"] = states[p.ID].ReplyTo
		return m
	})
	titles, err := s.st.EquippedTitles(r.Context(), authorIDs)
	if s.readError(w, r, err) {
		return
	}
	accepted, err := s.st.AcceptedReply(r.Context(), th.ID)
	if s.readError(w, r, err) {
		return
	}
	for i, p := range rows {
		out[i]["equippedTitle"] = titles[p.AuthorID]
		out[i]["accepted"] = accepted == p.ID
		caps := out[i]["capabilities"].(map[string]bool)
		caps["canAccept"] = s.canAcceptReply(r, p, th) && accepted == 0
		caps["canUnaccept"] = s.canAcceptReply(r, p, th) && accepted == p.ID
	}
	s.list(w, out, page, size, total)
}
func (s *Server) visiblePost(w http.ResponseWriter, r *http.Request, pid int64) (*store.Post, *store.Thread) {
	p, err := s.st.Post(r.Context(), pid)
	if s.readError(w, r, err) {
		return nil, nil
	}
	th := s.visibleThread(w, r, p.ThreadID)
	if th == nil {
		return nil, nil
	}
	if !s.canViewPost(r, p, th) {
		s.fail(w, r, 404, "NOT_FOUND", "内容不存在")
		return nil, nil
	}
	return p, th
}
func (s *Server) postGet(w http.ResponseWriter, r *http.Request) {
	p, th := s.visiblePost(w, r, pathID(r, "pid"))
	if p == nil {
		return
	}
	atts, err := s.st.UploadsForPosts(r.Context(), []int64{p.ID})
	if s.readError(w, r, err) {
		return
	}
	m := s.postResponse(r, p, th)
	badges, err := s.st.MemberSummaries(r.Context(), []int64{p.AuthorID})
	if s.readError(w, r, err) {
		return
	}
	m["authorLevel"] = badges[p.AuthorID]
	titles, err := s.st.EquippedTitles(r.Context(), []int64{p.AuthorID})
	if s.readError(w, r, err) {
		return
	}
	accepted, err := s.st.AcceptedReply(r.Context(), th.ID)
	if s.readError(w, r, err) {
		return
	}
	m["equippedTitle"] = titles[p.AuthorID]
	m["accepted"] = accepted == p.ID
	caps := m["capabilities"].(map[string]bool)
	caps["canAccept"] = s.canAcceptReply(r, p, th) && accepted == 0
	caps["canUnaccept"] = s.canAcceptReply(r, p, th) && accepted == p.ID
	m["attachments"] = mapRows(atts[p.ID], uploadDTO)
	uid := int64(0)
	if User(r) != nil {
		uid = User(r).ID
	}
	states, err := s.st.PostViewerStates(r.Context(), []int64{p.ID}, uid)
	if s.readError(w, r, err) {
		return
	}
	m["viewerHasLiked"] = states[p.ID].Liked
	m["replyTo"] = states[p.ID].ReplyTo
	s.respond(w, 200, m)
}
func (s *Server) searchGet(w http.ResponseWriter, r *http.Request) {
	page := pageOf(r)
	rows, total, err := s.st.Search(r.Context(), r.URL.Query().Get("q"), page, 20, store.SearchOpts{ForumID: queryID(r, "forumId"), Author: r.URL.Query().Get("author")})
	if s.readError(w, r, err) {
		return
	}
	s.list(w, mapRows(rows, func(h *store.SearchHit) map[string]any {
		return map[string]any{"threadId": idString(h.ThreadID), "title": h.Title, "forumId": idString(h.ForumID), "forumName": h.ForumName, "authorName": h.AuthorName, "createdAt": h.CreatedAt, "excerpt": h.Excerpt}
	}), page, 20, total)
}
func (s *Server) meGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	v, err := s.memberUser(r, User(r), true)
	if !s.readError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) userGet(w http.ResponseWriter, r *http.Request) {
	u, err := s.st.UserByID(r.Context(), pathID(r, "id"))
	if s.readError(w, r, err) {
		return
	}
	page := pageOf(r)
	var rows []*store.Thread
	var total int
	if r.URL.Query().Get("tab") == "replies" {
		rows, total, err = s.st.RecentRepliesOfUser(r.Context(), u.ID, 10, (page-1)*10)
	} else {
		rows, total, err = s.st.RecentThreadsOfUser(r.Context(), u.ID, 10, (page-1)*10)
	}
	if s.readError(w, r, err) {
		return
	}
	posts, likes, err := s.st.Reputation(r.Context(), u.ID)
	if s.readError(w, r, err) {
		return
	}
	user, err := s.memberUser(r, u, false)
	if s.readError(w, r, err) {
		return
	}
	threadRows, err := s.memberThreadRows(r, rows)
	if s.readError(w, r, err) {
		return
	}
	s.list(w, map[string]any{"user": user, "threads": threadRows, "reputation": map[string]int64{"posts": posts, "likes": likes}}, page, 10, total)
}
func (s *Server) favoritesGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.FavoritesOfUser(r.Context(), User(r).ID, page, 20)
	if s.readError(w, r, err) {
		return
	}
	ids := []int64{}
	for _, f := range rows {
		ids = append(ids, f.AuthorID)
	}
	badges, err := s.st.MemberSummaries(r.Context(), ids)
	if s.readError(w, r, err) {
		return
	}
	s.list(w, mapRows(rows, func(f *store.FavoriteRow) map[string]any {
		m := threadDTO(&f.Thread)
		m["authorLevel"] = badges[f.AuthorID]
		m["lastFloor"] = f.LastFloor
		m["hasNew"] = f.HasNew
		return m
	}), page, 20, total)
}
func (s *Server) draftsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	rows, err := s.st.DraftsOfUser(r.Context(), User(r).ID)
	if s.readError(w, r, err) {
		return
	}
	out := []map[string]any{}
	for _, d := range rows {
		out = append(out, map[string]any{"context": d.Context, "subject": d.Subject, "content": d.Content, "updatedAt": d.UpdatedAt})
	}
	s.respond(w, 200, out)
}
func (s *Server) historyGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	p, th := s.visiblePost(w, r, pathID(r, "pid"))
	if p == nil {
		return
	}
	if !canEditContent(User(r), p.AuthorID) && !s.canModerateThread(r, th) {
		s.fail(w, r, 403, "FORBIDDEN", "无权读取编辑历史")
		return
	}
	rows, err := s.st.PostEditsOf(r.Context(), p.ID)
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, mapRows(rows, func(e *store.PostEdit) map[string]any {
		return map[string]any{"id": idString(e.ID), "editorId": idString(e.EditorID), "editor": e.Editor, "content": e.ContentMD, "createdAt": e.CreatedAt}
	}))
}
func (s *Server) notificationsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	unread := r.URL.Query().Get("unread")
	if unread != "" && unread != "true" && unread != "false" {
		s.fail(w, r, 422, "VALIDATION_FAILED", "unread 必须为 true 或 false")
		return
	}
	page := pageOf(r)
	rows, total, err := s.st.NotificationPage(r.Context(), User(r).ID, page, 30, unread == "true")
	if s.readError(w, r, err) {
		return
	}
	s.list(w, mapRows(rows, func(n *store.Notification) map[string]any {
		return map[string]any{"id": idString(n.ID), "fromUserId": idString(n.FromUID), "fromName": n.FromName, "type": n.Type, "threadId": idString(n.ThreadID), "postId": idString(n.PostID), "excerpt": n.Excerpt, "read": n.Read, "createdAt": n.CreatedAt, "scope": n.Scope, "payload": n.Payload}
	}), page, 30, total)
}
func (s *Server) notificationsRead(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	all := r.PostFormValue("all") == "1" || r.PostFormValue("all") == "true"
	raw := r.PostForm["ids"]
	if (all && len(raw) > 0) || (!all && len(raw) == 0) || len(raw) > 100 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "提供 ids（最多 100 个）或 all=true")
		return
	}
	ids := make([]int64, 0, len(raw))
	for _, v := range raw {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			s.fail(w, r, 422, "VALIDATION_FAILED", "无效通知 ID")
			return
		}
		ids = append(ids, id)
	}
	n, err := s.st.ReadNotifications(r.Context(), User(r).ID, ids, all)
	if s.readError(w, r, err) {
		return
	}
	s.respond(w, 200, map[string]any{"read": true, "updated": n})
}
func (s *Server) smileysGet(w http.ResponseWriter, r *http.Request) {
	s.respond(w, 200, smiley.Groups())
}
func (s *Server) setupGet(w http.ResponseWriter, r *http.Request) {
	s.respond(w, 200, map[string]bool{"required": s.setupRequired()})
}
func (s *Server) captchaGet(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "captcha", 30, time.Minute) {
		s.fail(w, r, 429, "RATE_LIMITED", "请求过于频繁")
		return
	}
	id, _ := captcha.New()
	s.respond(w, 200, map[string]string{"id": id, "url": "/captcha/" + id})
}
func (s *Server) readRecord(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	th := s.visibleThread(w, r, pathID(r, "tid"))
	if th == nil {
		return
	}
	pid := formInt64(r, "postId")
	p, err := s.st.Post(r.Context(), pid)
	if s.readError(w, r, err) {
		return
	}
	if p.ThreadID != th.ID || !s.canViewPost(r, p, th) {
		s.fail(w, r, 404, "NOT_FOUND", "内容不存在")
		return
	}
	uid := User(r).ID
	if s.allowKey("read:"+idString(uid)+":"+idString(th.ID), 1, time.Minute) {
		s.st.IncView(th.ID)
		if err := s.st.RecordMemberRead(r.Context(), uid, p.ID, th.ID, p.Floor); s.readError(w, r, err) {
			return
		}
		if err := s.st.RecordMemberActivity(r.Context(), uid); s.readError(w, r, err) {
			return
		}
		if _, err := s.st.ProcessMemberEvents(r.Context(), 100); s.readError(w, r, err) {
			return
		}
		if err := s.st.UpgradeMember(r.Context(), uid); s.readError(w, r, err) {
			return
		}
	}
	s.respond(w, 200, map[string]bool{"recorded": true})
}
