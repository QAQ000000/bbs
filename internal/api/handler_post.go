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

// ---- 新主题 ----

func (s *Server) newThreadSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	if !s.allowKey("post:"+strconv.FormatInt(u.ID, 10), 10, time.Minute) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "发帖太快了，休息一下再试。")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	if !s.checkMustChangePassword(w, r) {
		return
	}
	fid := formInt64(r, "forumId")
	if fid <= 0 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "请选择版块")
		return
	}

	subject := r.PostFormValue("subject")
	content := r.PostFormValue("content")
	if msg := validateContent(subject, content, true); msg != "" {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg)
		return
	}

	_, err := s.st.Forum(r.Context(), fid)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusNotFound, "版块不存在", "该版块不存在。")
		return
	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "发帖失败", err.Error())
		return
	}

	ct := s.censorTexts(r, subject, content)
	subject, content = ct[0], ct[1]
	pending, reason := s.moderationDecision(r, u, fid, subject+" "+content)
	html := "" // 兼容旧存储参数；正文由独立前端渲染。
	ids, err := tagIDs(r)
	if s.communityError(w, r, err) {
		return
	}
	th, p, err := s.st.CreateTaggedThread(r.Context(), fid, u.ID, u.Username, subject, content, html, pending, reason, ids)
	if errors.Is(err, store.ErrCommunityInvalid) {
		s.communityError(w, r, err)
		return
	}
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "发帖失败", err.Error())
		return
	}
	s.linkUploads(r, u.ID, p.ID, content)
	s.st.SetPostIP(r.Context(), p.ID, remoteIP(r))
	// 待审核内容不进入事件层（SSE 订阅者含匿名访客），审批通过时再广播
	if !pending {
		s.broadcastPost("post.new", th, p)
		s.broadcastThread("thread.new", th)
		s.notifyMentions(r, u, content, th, p)
	}
	_ = s.st.SaveDraft(r.Context(), u.ID, "new:"+strconv.FormatInt(fid, 10), "")
	s.respond(w, http.StatusCreated, map[string]any{"threadId": idString(th.ID), "postId": idString(p.ID), "pending": pending, "version": p.Version})
}

// ---- 回复 ----

func (s *Server) replySubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	if !s.allowKey("post:"+strconv.FormatInt(u.ID, 10), 10, time.Minute) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "发帖太快了，休息一下再试。")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	if !s.checkMustChangePassword(w, r) {
		return
	}
	tid := pathID(r, "tid")
	visible := s.visibleThread(w, r, tid)
	if visible == nil {
		return
	}
	content := r.PostFormValue("content")
	if msg := validateContent("", content, false); msg != "" {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg)
		return
	}
	content = s.censorTexts(r, content)[0]
	pending, reason := s.moderationDecision(r, u, visible.ForumID, content)
	html := "" // 兼容旧存储参数；正文由独立前端渲染。
	var replyTo int64
	if raw := r.PostFormValue("replyToPostId"); raw != "" && raw != "0" {
		var err error
		replyTo, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || replyTo <= 0 {
			s.fail(w, r, 422, "VALIDATION_FAILED", "无效回复目标")
			return
		}
		target, targetThread := s.visiblePost(w, r, replyTo)
		if target == nil {
			return
		}
		if targetThread.ID != tid || target.Pending {
			s.fail(w, r, 404, "NOT_FOUND", "回复目标不存在")
			return
		}
	}
	th, p, err := s.st.CreateReplyTo(r.Context(), tid, u.ID, u.Username, content, html, pending, reason, replyTo)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusNotFound, "主题不存在", "该主题不存在或已锁定。")
		return
	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "回复失败", err.Error())
		return
	}
	s.linkUploads(r, u.ID, p.ID, content)
	s.st.SetPostIP(r.Context(), p.ID, remoteIP(r))
	if !pending {
		s.broadcastPost("post.new", th, p)
		// 待审主题的行不能进版块页 SSE（游客订阅 f: 版块主题）：
		// 主题可见后由审批路径广播
		if !th.Pending {
			s.broadcastThread("thread.update", th)
			mentioned := s.notifyMentions(r, u, content, th, p)
			s.notifyReply(r, u, th, p, mentioned)
		}
	}
	_ = s.st.SaveDraft(r.Context(), u.ID, "reply:"+strconv.FormatInt(tid, 10), "")
	s.respond(w, http.StatusCreated, map[string]any{"threadId": idString(th.ID), "postId": idString(p.ID), "pending": pending, "version": p.Version})
}

// ---- 编辑 ----

func (s *Server) editSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	if !s.allowKey("post:"+strconv.FormatInt(u.ID, 10), 10, time.Minute) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "发帖太快了，休息一下再试。")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	if !s.checkMustChangePassword(w, r) {
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusNotFound, "内容不存在", "该楼层不存在或已被删除。")
		return
	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if !canEditContent(u, p.AuthorID) {
		s.fail(w, r, http.StatusForbidden, "没有权限", "只能编辑自己的内容。")
		return
	}
	// 编辑冲突检测：携带版本落后于当前版本则拒绝；
	// 版本号继续传入 UpdatePost 在事务（行锁）内做权威校验，封死并发窗口
	v, _ := strconv.Atoi(r.PostFormValue("version"))
	if v <= 0 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "必须提供当前 version")
		return
	}
	if v != p.Version {
		s.fail(w, r, http.StatusConflict, "内容已被他人更新",
			"你编辑期间该楼层被其他会话修改过（当前版本 "+strconv.Itoa(p.Version)+"，你基于版本 "+strconv.Itoa(v)+"）。请刷新查看最新内容后重新编辑。")
		return
	}
	subject := r.PostFormValue("subject")
	content := r.PostFormValue("content")
	if msg := validateContent(subject, content, p.Floor == 1); msg != "" {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg)
		return
	}
	// 编辑复检：过审内容被编辑时按规则重新入队（堵住"过审后编辑加链接"的绕过）；
	// 已在待审队列的内容保持待审
	reenqueue := false
	reason := ""
	if !p.Pending {
		existingThread, err := s.st.Thread(r.Context(), p.ThreadID)
		if s.readError(w, r, err) {
			return
		}
		reenqueue, reason = s.moderationDecision(r, u, existingThread.ForumID, subject+" "+content)
	}
	ct := s.censorTexts(r, subject, content)
	subject, content = ct[0], ct[1]
	html := "" // 兼容旧存储参数；正文由独立前端渲染。
	// 版本校验、改前快照与内容更新在 UpdatePost 事务内完成：
	// 并发编辑只有一个请求成功，后写不覆盖先写
	p2, th, err := s.st.UpdatePost(r.Context(), pid, v, u.ID, p.ContentMD, subject, content, html)
	if errors.Is(err, store.ErrEditConflict) {
		s.fail(w, r, http.StatusConflict, "内容已被他人更新",
			"你编辑期间该楼层被其他会话修改过，请刷新查看最新内容后重新编辑。")
		return
	}
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	s.linkUploads(r, u.ID, pid, content)
	if reenqueue {
		if err := s.st.SetPostPendingModeration(r.Context(), pid, reason); err != nil {
			s.fail(w, r, 500, "INTERNAL_ERROR", err.Error())
			return
		}

	} else if !p2.Pending && !th.Pending {
		// 只有最终公开的楼层才广播：编辑待审内容不能把完整正文
		// 推给订阅了该主题的游客
		s.broadcastPost("post.edit", th, p2)
		if p.Floor == 1 && !th.Pending {
			s.broadcastThread("thread.update", th)
		}
	}
	_ = s.st.SaveDraft(r.Context(), u.ID, "edit:"+strconv.FormatInt(pid, 10), "")

	s.respond(w, http.StatusOK, map[string]any{"postId": idString(p2.ID), "threadId": idString(th.ID), "version": p2.Version, "pending": reenqueue || p2.Pending})
}

// ---- 删除 ----

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请返回刷新后重试。")
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	if !s.checkMustChangePassword(w, r) {
		return
	}
	pid := pathID(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusNotFound, "内容不存在", "该楼层不存在或已被删除。")
		return
	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	// 删除权限：自己的内容（ContentDeleteOwn）始终允许；
	// ContentDeleteAny 的版主限其管辖版块（管理员不限），与前台按钮口径一致
	th, err := s.st.Thread(r.Context(), p.ThreadID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "删除失败", err.Error())
		return
	}
	del := perm.Allowed(perm.RoleFromGroupID(u.GroupID), perm.ContentDeleteOwn) && u.ID == p.AuthorID
	if !del && perm.Allowed(perm.RoleFromGroupID(u.GroupID), perm.ContentDeleteAny) {
		del = hasPoint(u, perm.AdminPanel) || inForumScope(s.staffForumScope(r), th.ForumID)
	}
	if !del {
		s.fail(w, r, http.StatusForbidden, "没有权限", "只能删除自己的内容，或管辖版块内的内容。")
		return
	}
	deletedThread, tid, err := s.st.DeletePost(r.Context(), pid)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "删除失败", err.Error())
		return
	}
	if deletedThread {
		// 用删除前取出的主题快照广播：删后再查必然不存在，
		// 会连带跳过缓存失效，游客最长 60s 继续看到旧全文
		s.publish("t:"+strconv.FormatInt(tid, 10), eventBody{Type: "thread.deleted", TID: tid})
		s.broadcastThread("thread.delete", th)
		result.Message = "主题已删除"
		s.respond(w, http.StatusOK, result)
		return
	}
	// th 已在权限校验时取出（同一主题），直接广播
	s.broadcastPost("post.delete", th, &store.Post{ID: pid, ThreadID: tid, Floor: p.Floor})
	s.broadcastThread("thread.update", th)
	s.respond(w, http.StatusOK, result)
}

// 审核原因码。
const (
	reasonManual          = "manual"           // 站点开启了发帖审核
	reasonNewUserLink     = "newuser_link"     // 新用户内容含链接
	reasonEmailUnverified = "email_unverified" // 邮箱验证开启但用户邮箱未验证
)

// emailGateEnabled 邮箱验证闸门是否生效：开关开启且 SMTP 可用（否则自动降级）。
func (s *Server) emailGateEnabled() bool {
	return s.st.Settings(context.Background()).EmailVerifyEnabled && s.mailer.Enabled()
}

// moderationDecision 决定该用户此内容是否进入审核队列及原因：
// 站点审核开关（manual）；或新用户内容含链接（newuser_link）；或邮箱验证开启但
// 用户邮箱未验证且内容含链接（email_unverified，SMTP 关闭时闸门自动失效）。
func (s *Server) moderationDecision(r *http.Request, u *store.User, fid int64, content string) (bool, string) {
	if u == nil || hasPoint(u, perm.ContentModerate) && (hasPoint(u, perm.AdminPanel) || membershipOf(r).Staff[fid]) {
		return false, ""
	}
	hasLink := strings.Contains(content, "http://") || strings.Contains(content, "https://")
	if hasLink {
		if !memberRuleAllowed(r, "post.link.direct", fid) {
			return true, reasonNewUserLink
		}
		if s.emailGateEnabled() && u.Email != "" && !u.EmailVerified {
			return true, reasonEmailUnverified
		}
	}
	// Only the configured membership permission exempts routine moderation.
	if s.sets(r).ModerateEnabled && !memberRuleAllowed(r, "post.skip.moderate", fid) {
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

// ---- Markdown 实时预览 ----
