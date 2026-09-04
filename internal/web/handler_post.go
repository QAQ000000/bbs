// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/markdown"
	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// 内容校验：标题 1-80 字符，正文 1-30000 字符。
func validateContent(subject, content string, requireSubject bool) string {
	subject = strings.TrimSpace(subject)
	if requireSubject {
		if subject == "" {
			return "请输入标题"
		}
		if utf8.RuneCountInString(subject) > 80 {
			return "标题不能超过 80 个字符"
		}
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "内容不能为空"
	}
	if utf8.RuneCountInString(content) > 30000 {
		return "内容不能超过 30000 个字符"
	}
	return ""
}

// editorData 编辑器页面（新主题/回复/编辑共用）的公共数据。
type editorData struct {
	Common
	Action     string          // 提交地址
	Forum      *store.Forum    // 新主题时显示目标版块
	Thread     *store.Thread   // 回复/编辑时显示来源主题
	Subject    string
	Content    string
	EditorID   string
	Version    int    // 编辑时携带的版本号（0=新发内容）
	DraftContext string // 服务端草稿上下文
	UploadEnabled bool
	MaxImageMB  int
	MaxFileMB   int
	Smileys    []SmileyGroup
	Error      string
	IsThreadOp bool // 是否新主题/首楼编辑（显示标题框）
}

func (s *Server) editorCommon(r *http.Request) editorData {
	sets := s.sets(r)
	return editorData{Common: s.common(r), Smileys: SmileyGroups(), EditorID: "e",
		UploadEnabled: sets.UploadEnabled, MaxImageMB: sets.MaxImageMB, MaxFileMB: sets.MaxFileMB}
}

// ---- 新主题 ----

func (s *Server) newThreadForm(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	fid, _ := strconv.ParseInt(r.URL.Query().Get("fid"), 10, 64)
	forum, err := s.st.Forum(r.Context(), fid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "版块不存在", "请从正确的入口发帖。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	d := s.editorCommon(r)
	d.Action = "/new?fid=" + strconv.FormatInt(fid, 10)
	d.Forum = forum
	d.Common.Title = "发表新主题"
	d.IsThreadOp = true
	d.DraftContext = "new:" + strconv.FormatInt(fid, 10)
	d.CSRF = s.anonCSRFifNeeded(r, w)
	_ = s.rd.Render(w, "page_editor.html", &d)
}

func (s *Server) anonCSRFifNeeded(r *http.Request, w http.ResponseWriter) string {
	if Session(r) != nil {
		return Session(r).CSRF
	}
	return s.anonCSRF(r, w)
}

func (s *Server) newThreadSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	if !s.allowKey("post:"+strconv.FormatInt(u.ID, 10), 10, time.Minute) ||
		!s.allowKey("postd:"+strconv.FormatInt(u.ID, 10), 100, 24*time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "发帖太快了，休息一下再试。")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	fid, _ := strconv.ParseInt(r.URL.Query().Get("fid"), 10, 64)
	subject := r.PostFormValue("subject")
	content := r.PostFormValue("content")
	if msg := validateContent(subject, content, true); msg != "" {
		s.formError(w, r, "/new?fid="+strconv.FormatInt(fid, 10), msg)
		return
	}

	forum, err := s.st.Forum(r.Context(), fid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "版块不存在", "该版块不存在。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "发帖失败", err.Error())
		return
	}
	if forumHasClosed(forum) {
		s.renderError(w, r, http.StatusForbidden, "无法发帖", "该版块已关闭发帖。")
		return
	}

	ct := s.censorTexts(r, subject, content)
	subject, content = ct[0], ct[1]
	pending, reason := s.moderationDecision(r, u, subject+" "+content)
	html := markdown.Render(content)
	th, p, err := s.st.CreateThread(r.Context(), fid, u.ID, u.Username, subject, content, html, pending, reason)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "发帖失败", err.Error())
		return
	}
	// 待审核内容不进入事件层（SSE 订阅者含匿名访客），审批通过时再广播
	if !pending {
		s.broadcastPost("post.new", th, p)
		s.broadcastThread("thread.new", th)
		s.notifyMentions(r, u, content, th, p)
	}
	_ = s.st.SaveDraft(r.Context(), u.ID, "new:"+strconv.FormatInt(fid, 10), "")
	s.bustPageCache()
	if pending {
		s.setFlash(w, "内容已提交，审核通过后将公开展示。")
	}
	http.Redirect(w, r, ThreadURL(th.ID, 1), http.StatusFound)
}

func forumHasClosed(*store.Forum) bool { return false }

// ---- 回复 ----

func (s *Server) replyForm(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	tid := pathID(r, "tid")
	th, err := s.st.Thread(r.Context(), tid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "主题不存在", "该主题不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if th.Closed {
		s.renderError(w, r, http.StatusForbidden, "无法回复", "该主题已锁定。")
		return
	}
	d := s.editorCommon(r)
	d.Action = "/reply/" + strconv.FormatInt(tid, 10)
	d.Thread = th
	d.Common.Title = "回复主题：" + th.Title
	d.DraftContext = "reply:" + strconv.FormatInt(tid, 10)
	_ = s.rd.Render(w, "page_editor.html", &d)
}

func (s *Server) replySubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	tid := pathID(r, "tid")
	content := r.PostFormValue("content")
	if msg := validateContent("", content, false); msg != "" {
		s.formError(w, r, "/reply/"+strconv.FormatInt(tid, 10), msg)
		return
	}
	content = s.censorTexts(r, content)[0]
	pending, reason := s.moderationDecision(r, u, content)
	html := markdown.Render(content)
	th, p, err := s.st.CreateReply(r.Context(), tid, u.ID, u.Username, content, html, pending, reason)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "主题不存在", "该主题不存在或已锁定。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "回复失败", err.Error())
		return
	}
	if !pending {
		s.broadcastPost("post.new", th, p)
		s.broadcastThread("thread.update", th)
		s.notifyMentions(r, u, content, th, p)
	}
	_ = s.st.SaveDraft(r.Context(), u.ID, "reply:"+strconv.FormatInt(tid, 10), "")
	s.bustPageCache()
	if pending {
		s.setFlash(w, "回复已提交，审核通过后将公开展示。")
	}

	lastPage := (th.PostCount + s.sets(r).PostsPerPage - 1) / s.sets(r).PostsPerPage
	http.Redirect(w, r, ThreadURL(th.ID, lastPage)+"#post"+strconv.FormatInt(p.ID, 10), http.StatusFound)
}

// ---- 编辑 ----

func (s *Server) editForm(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "内容不存在", "该楼层不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	u := User(r)
	if !u.IsAdmin() && u.ID != p.AuthorID {
		s.renderError(w, r, http.StatusForbidden, "没有权限", "只能编辑自己的内容。")
		return
	}
	th, err := s.st.Thread(r.Context(), p.ThreadID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	d := s.editorCommon(r)
	d.Action = "/edit/" + strconv.FormatInt(pid, 10)
	d.Thread = th
	d.Subject = firstPostTitle(p.Floor, th.Title)
	d.Content = p.ContentMD
	d.Common.Title = "编辑内容"
	d.IsThreadOp = p.Floor == 1
	d.Version = p.Version
	d.DraftContext = "edit:" + strconv.FormatInt(pid, 10)
	_ = s.rd.Render(w, "page_editor.html", &d)
}

func firstPostTitle(floor int, title string) string {
	if floor == 1 {
		return title
	}
	return ""
}

func (s *Server) editSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "内容不存在", "该楼层不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if !canEditContent(u, p.AuthorID) {
		s.renderError(w, r, http.StatusForbidden, "没有权限", "只能编辑自己的内容。")
		return
	}
	// 编辑冲突检测（版本号比对）：携带版本落后于当前版本则拒绝
	if v, _ := strconv.Atoi(r.PostFormValue("version")); v > 0 && v != p.Version {
		s.renderError(w, r, http.StatusConflict, "内容已被他人更新",
			"你编辑期间该楼层被其他会话修改过（当前版本 "+strconv.Itoa(p.Version)+"，你基于版本 "+strconv.Itoa(v)+"）。请刷新查看最新内容后重新编辑。")
		return
	}
	subject := r.PostFormValue("subject")
	content := r.PostFormValue("content")
	if msg := validateContent(subject, content, p.Floor == 1); msg != "" {
		s.formError(w, r, "/edit/"+strconv.FormatInt(pid, 10), msg)
		return
	}
	// 编辑复检：过审内容被编辑时按规则重新入队（堵住"过审后编辑加链接"的绕过）；
	// 已在待审队列的内容保持待审
	reenqueue := false
	reason := ""
	if !p.Pending {
		reenqueue, reason = s.moderationDecision(r, u, subject+" "+content)
	}
	ct := s.censorTexts(r, subject, content)
	subject, content = ct[0], ct[1]
	html := markdown.Render(content)
	p2, th, err := s.st.UpdatePost(r.Context(), pid, subject, content, html)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	if reenqueue {
		_ = s.st.SetPostPendingModeration(r.Context(), pid, reason)
		s.setFlash(w, "内容已提交重新审核，审核通过前不可见")
	} else {
		s.broadcastPost("post.edit", th, p2)
		if p.Floor == 1 {
			s.broadcastThread("thread.update", th)
		}
	}
	_ = s.st.SaveDraft(r.Context(), u.ID, "edit:"+strconv.FormatInt(pid, 10), "")
	s.bustPageCache()
	http.Redirect(w, r, ThreadURL(th.ID, 1)+"#post"+strconv.FormatInt(p2.ID, 10), http.StatusFound)
}

// ---- 删除 ----

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "内容不存在", "该楼层不存在或已被删除。")
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if !canDeleteContent(u, p.AuthorID) {
		s.renderError(w, r, http.StatusForbidden, "没有权限", "只能删除自己的内容。")
		return
	}
	deletedThread, tid, err := s.st.DeletePost(r.Context(), pid)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "删除失败", err.Error())
		return
	}
	if deletedThread {
		th, err := s.st.Thread(r.Context(), tid)
		if err == nil {
			s.publish("t:"+strconv.FormatInt(tid, 10), eventBody{Type: "thread.deleted", TID: tid})
			s.broadcastThread("thread.delete", th)
		}
		s.setFlash(w, "主题已删除")
		http.Redirect(w, r, r.PostFormValue("back"), http.StatusSeeOther)
		return
	}
	th, err := s.st.Thread(r.Context(), tid)
	if err == nil {
		s.broadcastPost("post.delete", th, &store.Post{ID: pid, ThreadID: tid, Floor: p.Floor})
		s.broadcastThread("thread.update", th)
	}
	http.Redirect(w, r, ThreadURL(tid, 1), http.StatusSeeOther)
}

// 审核原因码。
const (
	reasonManual     = "manual"       // 站点开启了发帖审核
	reasonNewUserLink = "newuser_link" // 新用户内容含链接
)

// ReasonLabels 原因码中文说明（审核队列展示）。
var ReasonLabels = map[string]string{
	reasonManual:      "发帖审核开关开启",
	reasonNewUserLink: "新用户内容含链接",
}

// moderationDecision 决定该用户此内容是否进入审核队列及原因：
// 站点审核开关（manual）；或新用户（发帖<5 且注册<7天）内容含链接（newuser_link，不受开关限制）。
func (s *Server) moderationDecision(r *http.Request, u *store.User, content string) (bool, string) {
	if u == nil || isStaff(u) {
		return false, ""
	}
	hasLink := strings.Contains(content, "http://") || strings.Contains(content, "https://")
	if hasLink && !perm.TrustAllowed(perm.TrustLevel(u.TrustLevel), perm.PostLinkDirect) {
		return true, reasonNewUserLink
	}
	if s.sets(r).ModerateEnabled {
		return true, reasonManual
	}
	return false, ""
}

// censorTexts 敏感词替换（多段文本）。
func (s *Server) censorTexts(r *http.Request, texts ...string) []string {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = s.st.ApplyCensor(r.Context(), t)
	}
	return out
}

// formError 渲染编辑器页面并带错误信息（保留用户输入）。
func (s *Server) formError(w http.ResponseWriter, r *http.Request, action, msg string) {
	// 简化处理：闪现消息 + 回表单
	s.setFlash(w, msg)
	http.Redirect(w, r, action, http.StatusSeeOther)
}

// ---- Markdown 实时预览 ----

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := jsonDecode(r, &req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(map[string]string{"html": markdown.Render(req.Content)})
	_, _ = w.Write(b)
}

func jsonDecode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
