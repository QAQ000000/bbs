// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

type membershipKey struct{}
type membershipContext struct {
	Config store.MembershipConfig
	Member *store.MemberState
	Forums map[int64]bool
	Staff  map[int64]bool
	Quota  map[string]int64
	Banned bool
}

func membershipOf(r *http.Request) *membershipContext {
	m, _ := r.Context().Value(membershipKey{}).(*membershipContext)
	return m
}
func (s *Server) loadMembership(r *http.Request) (*http.Request, error) {
	if r.Context().Value(settingsKey{}) == nil && !strings.HasPrefix(r.URL.Path, "/api/v1/admin/settings") {
		var err error
		r, err = s.loadSettings(r)
		if err != nil {
			return r, err
		}
	}
	uid := int64(0)
	if u := User(r); u != nil {
		uid = u.ID
	}
	a, err := s.st.MembershipAccess(r.Context(), uid)
	if err != nil {
		return r, err
	}
	c := a.Config
	m := &membershipContext{Config: c, Member: a.Member, Banned: a.Banned, Quota: a.Quota, Forums: map[int64]bool{}, Staff: map[int64]bool{}}
	if hasPoint(User(r), perm.ContentModerate) {
		for _, fid := range a.Moderates {
			m.Staff[fid] = true
		}
	}
	visible := []int64{}
	for _, id := range a.Forums {
		var level *store.MemberLevel
		if m.Member != nil {
			level = &m.Member.Level
		}
		allow := store.ForumReadAllowed(c, level, hasPoint(User(r), perm.AdminPanel), m.Staff[id], id)
		m.Forums[id] = allow
		if allow {
			visible = append(visible, id)
		}
	}
	ctx := context.WithValue(store.WithMembershipSnapshot(r.Context(), c), membershipKey{}, m)
	// Governance APIs already apply their own operation and moderation scopes.
	if !strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		ctx = store.WithVisibleForums(ctx, visible)
	}
	return r.WithContext(ctx), nil
}
func (s *Server) membershipMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/api/status" || strings.HasPrefix(p, "/api/v1/health/") || p == "/api/v1/setup" || strings.HasPrefix(p, "/captcha/") || strings.HasPrefix(p, "/avatar/") || strings.HasPrefix(p, "/smiley/") {
			next.ServeHTTP(w, r)
			return
		}
		rr, err := s.loadMembership(r)
		if s.readError(w, r, err) {
			return
		}
		next.ServeHTTP(w, rr)
	})
}
func canReadForum(r *http.Request, fid int64) bool {
	m := membershipOf(r)
	return m != nil && m.Forums[fid]
}

type memberDecision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	Action  string `json:"action"`
	Limit   int64  `json:"limit"`
	Used    int64  `json:"used"`
}

func (s *Server) memberDecision(r *http.Request, action string, fid int64, p *store.Post, th *store.Thread) memberDecision {
	d := memberDecision{Action: action, Reason: "允许", Limit: -1}
	deny := func(why string) memberDecision { d.Allowed = false; d.Reason = why; return d }
	m := membershipOf(r)
	if m == nil {
		return deny("会员状态不可用")
	}
	if fid > 0 && !m.Forums[fid] {
		return deny("无权访问该版块")
	}
	if User(r) != nil && User(r).IsBlocked() {
		return deny("账号已封禁")
	}
	if th != nil && !s.canViewThread(r, th) {
		return deny("内容不可见")
	}
	if p != nil && th != nil && !s.canViewPost(r, p, th) {
		return deny("内容不可见")
	}
	if action == "forum.read" {
		d.Allowed = true
		return d
	}
	u := User(r)
	if u == nil {
		if action == "attachment.download" {
			if !m.Config.GuestPermissions[action] {
				return deny("游客无权下载附件")
			}
			if f, ok := m.Config.Forum(fid); ok {
				for _, a := range f.Denied {
					if a == action {
						return deny("版块禁止此操作")
					}
				}
			}
			d.Allowed = true
			return d
		}
		return deny("请先登录")
	}
	if u.IsBlocked() {
		return deny("账号已封禁")
	}
	if action != "attachment.download" && (m.Banned || u.MustChangePassword) {
		return deny("账号禁言或需修改初始密码")
	}
	if m.Member == nil {
		return deny("会员状态不可用")
	}
	l := m.Member.Level
	// Only explicit content-management operations bypass member restrictions.
	staff := hasPoint(u, perm.AdminPanel) || m.Staff[fid]
	manage := staff && ((action == "post.edit" && hasPoint(u, perm.ContentEditAny)) || (action == "post.delete" && hasPoint(u, perm.ContentDeleteAny)))
	if !manage && !l.Permissions[action] {
		return deny("当前等级未开放此操作")
	}
	if f, ok := m.Config.Forum(fid); ok && !manage {
		for _, a := range f.Denied {
			if a == action {
				return deny("版块禁止此操作")
			}
		}
	}
	if action == "post.reply" && th != nil && th.Closed {
		return deny("主题已锁定")
	}
	if p != nil {
		switch action {
		case "post.edit":
			if !manage && !(p.AuthorID == u.ID && hasPoint(u, perm.ContentEditOwn)) {
				return deny("只能编辑本人内容")
			}
			if !manage && l.Limits.EditMinutes >= 0 && (l.Limits.EditMinutes == 0 || time.Since(p.CreatedAt).Minutes() > float64(l.Limits.EditMinutes)) {
				return deny("已超过编辑时限")
			}
		case "post.delete":
			if !manage && !(p.AuthorID == u.ID && hasPoint(u, perm.ContentDeleteOwn)) {
				return deny("只能删除本人内容")
			}
		case "post.like", "post.report":
			if p.AuthorID == u.ID {
				return deny("不能对本人内容执行此操作")
			}
		}
	}
	switch action {
	case "thread.create":
		d.Limit = l.Limits.ThreadsPerDay
	case "post.reply":
		d.Limit = l.Limits.RepliesPerDay
	case "upload.image", "upload.file":
		if !hasPoint(u, perm.UploadUse) || !s.sets(r).UploadEnabled {
			return deny("站点或角色禁止上传")
		}
		d.Limit = l.Limits.UploadsPerDay
	}
	key := action
	if strings.HasPrefix(action, "upload.") {
		key = "upload"
	}
	d.Used = m.Quota[key]
	if d.Limit >= 0 && d.Used >= d.Limit {
		return deny("已达到每日额度")
	}
	d.Allowed = true
	return d
}

type memberResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *memberResponseWriter) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
	}
	w.ResponseWriter.WriteHeader(n)
}
func (w *memberResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}

// Called after JSON/form parsing and CSRF verification. Existing business
// validation remains; quota reservations are refunded on any failed response.
func (s *Server) memberAction(w http.ResponseWriter, r *http.Request, h http.HandlerFunc) {
	action := ""
	var fid int64
	var p *store.Post
	var th *store.Thread
	switch r.Pattern {
	case "POST /api/v1/threads":
		action = "thread.create"
		fid = formInt64(r, "forumId")
	case "POST /api/v1/threads/{tid}/posts":
		action = "post.reply"
	case "PATCH /api/v1/posts/{pid}":
		action = "post.edit"
	case "DELETE /api/v1/posts/{pid}":
		action = "post.delete"
	case "POST /api/v1/posts/{pid}/like":
		action = "post.like"
	case "POST /api/v1/posts/{pid}/reports":
		action = "post.report"
	case "POST /api/v1/threads/{tid}/favorite":
		action = "thread.favorite"
	}
	if action == "" {
		h(w, r)
		return
	}
	if pid := pathID(r, "pid"); pid > 0 {
		p, th = s.visiblePost(w, r, pid)
		if p == nil {
			return
		}
	} else if tid := pathID(r, "tid"); tid > 0 {
		th = s.visibleThread(w, r, tid)
		if th == nil {
			return
		}
	}
	if th != nil {
		fid = th.ForumID
	}
	d := s.memberDecision(r, action, fid, p, th)
	if !d.Allowed {
		if d.Limit >= 0 && d.Used >= d.Limit {
			s.fail(w, r, 429, "MEMBER_QUOTA_EXCEEDED", d.Reason)
		} else {
			s.fail(w, r, 403, "MEMBER_PERMISSION_DENIED", d.Reason)
		}
		return
	}
	if action == "thread.create" || action == "post.reply" || action == "post.edit" {
		m := membershipOf(r)
		limit := m.Member.Level.Limits.AttachmentsPerPost
		unique := map[string]bool{}
		for _, v := range uploadsPathRe.FindAllString(r.PostFormValue("content"), -1) {
			unique[v] = true
		}
		if limit >= 0 && int64(len(unique)) > limit {
			s.fail(w, r, 422, "ATTACHMENT_LIMIT", "超过等级允许的每帖附件数")
			return
		}
	}
	day, err := s.st.ReserveMemberQuota(r.Context(), User(r).ID, action, 1, d.Limit)
	if err != nil {
		s.memberError(w, r, err)
		return
	}
	rw := &memberResponseWriter{ResponseWriter: w}
	committed := false
	r = r.WithContext(store.WithMemberCommitMarker(r.Context(), func() { committed = true }))
	defer func() {
		if !committed && (rw.status == 0 || rw.status >= 400) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := s.st.RefundMemberQuota(ctx, User(r).ID, day, action, 1); e != nil {
				s.log.Error("refund member quota", "err", e)
			}
		}
	}()
	h(rw, r)
}
func (s *Server) memberError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrMembershipConflict):
		s.fail(w, r, 409, "MEMBERSHIP_CONFLICT", err.Error())
	case errors.Is(err, store.ErrMemberQuota):
		s.fail(w, r, 429, "MEMBER_QUOTA_EXCEEDED", err.Error())
	default:
		s.readError(w, r, err)
	}
}

func memberSignatureAllowed(r *http.Request, value string) bool {
	m := membershipOf(r)
	return m != nil && m.Member != nil && (m.Member.Level.Limits.SignatureLength < 0 || int64(utf8.RuneCountInString(value)) <= m.Member.Level.Limits.SignatureLength)
}
func (s *Server) memberThreadRows(r *http.Request, rows []*store.Thread) ([]map[string]any, error) {
	if len(rows) == 0 {
		return []map[string]any{}, nil
	}
	tids := []int64{}
	for _, v := range rows {
		tids = append(tids, v.ID)
	}
	tags, err := s.st.ThreadTags(r.Context(), tids)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, v := range rows {
		ids = append(ids, v.AuthorID)
	}
	badges, err := s.st.MemberSummaries(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	titles, err := s.st.EquippedTitles(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	engagement, err := s.engagementMetadata(r, tids)
	if err != nil {
		return nil, err
	}
	rules, err := s.st.EngagementConfig(r.Context())
	if err != nil {
		return nil, err
	}
	// 列表摘要与封面：一次批量投影，SQL 内已按待审 / 删除过滤。
	previews, err := s.st.ThreadPreviews(r.Context(), tids)
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(t *store.Thread) map[string]any {
		v := threadDTO(t)
		v["authorLevel"] = badges[t.AuthorID]
		v["equippedTitle"] = titles[t.AuthorID]
		v["tags"] = tags[t.ID]
		v["excerpt"] = previews[t.ID].Excerpt
		v["coverUrl"] = previews[t.ID].CoverURL
		s.threadEngagementResponse(r, t, v, engagement[t.ID], rules)
		return v
	}), nil
}
func (s *Server) memberUser(r *http.Request, u *store.User, private bool) (map[string]any, error) {
	var v map[string]any
	if private {
		v = privateUser(u)
	} else {
		v = publicUser(u)
	}
	if u == nil {
		return v, nil
	}
	badges, err := s.st.MemberSummaries(r.Context(), []int64{u.ID})
	if err != nil {
		return nil, err
	}
	v["level"] = badges[u.ID]
	titles, err := s.st.EquippedTitles(r.Context(), []int64{u.ID})
	if err != nil {
		return nil, err
	}
	v["equippedTitle"] = titles[u.ID]
	return v, nil
}
func parseMemberID(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, errors.New("无效用户 ID")
	}
	return v, nil
}

func memberRuleAllowed(r *http.Request, action string, fid int64) bool {
	m := membershipOf(r)
	if m == nil || m.Member == nil || !m.Member.Level.Permissions[action] {
		return false
	}
	if f, ok := m.Config.Forum(fid); ok {
		for _, a := range f.Denied {
			if a == action {
				return false
			}
		}
	}
	return true
}

func (s *Server) memberForumResponse(r *http.Request, f *store.Forum) map[string]any {
	m := forumDTO(f)
	m["capabilities"] = map[string]bool{"canCreateThread": s.memberDecision(r, "thread.create", f.ID, nil, nil).Allowed}
	return m
}
func (s *Server) memberCategoriesResponse(r *http.Request, cats []*store.Category) []map[string]any {
	out := categoriesDTO(cats)
	for i, c := range cats {
		rows := out[i]["forums"].([]map[string]any)
		for j, f := range c.Forums {
			rows[j] = s.memberForumResponse(r, f)
		}
	}
	return out
}

func (s *Server) effectiveMemberLimits(r *http.Request) store.MemberLimits {
	m := membershipOf(r)
	l := m.Member.Level.Limits
	sets := s.sets(r)
	capAt := func(value, hard int64) int64 {
		if value < 0 || value > hard {
			return hard
		}
		return value
	}
	l.ImageBytes = capAt(l.ImageBytes, int64(sets.MaxImageMB)<<20)
	l.FileBytes = capAt(l.FileBytes, int64(sets.MaxFileMB)<<20)
	l.SignatureLength = capAt(l.SignatureLength, 200)
	return l
}
