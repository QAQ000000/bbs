package api

import (
	"dzforum/internal/store"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) messageSend(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	to := pathID(r, "id")
	if !s.checkNotBanned(w, r) || !s.checkMustChangePassword(w, r) {
		return
	}
	if !s.allowKey("message:"+idString(u.ID), 30, time.Minute) {
		s.fail(w, r, 429, "MESSAGE_RATE_LIMITED", "发送过于频繁")
		return
	}
	body := strings.TrimSpace(s.censorTexts(r, r.PostFormValue("body"))[0])
	if to <= 0 || to == u.ID || utf8.RuneCountInString(body) == 0 || utf8.RuneCountInString(body) > 5000 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效私信内容")
		return
	}
	msg, e := s.st.SendDirectMessage(r.Context(), u.ID, to, body)
	if errors.Is(e, store.ErrMessageBlocked) {
		s.fail(w, r, 403, "MESSAGE_BLOCKED", "对方已屏蔽你")
		return
	}
	if errors.Is(e, store.ErrMessageReplyRequired) {
		s.fail(w, r, 409, "MESSAGE_REPLY_REQUIRED", "请等待对方回复")
		return
	}
	if s.communityError(w, r, e) {
		return
	}
	s.respond(w, 201, msg)
}
func (s *Server) messagesGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	page := pageOf(r)
	rows, total, e := s.st.Conversations(r.Context(), u.ID, page)
	if !s.readError(w, r, e) {
		s.list(w, rows, page, 30, total)
	}
}
func (s *Server) conversationMessagesGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	cid := pathID(r, "cid")
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			s.fail(w, r, 422, "VALIDATION_FAILED", "无效消息游标")
			return
		}
		before = v
	}
	rows, e := s.st.ConversationMessages(r.Context(), u.ID, cid, before)
	if !s.readError(w, r, e) {
		s.respond(w, 200, rows)
	}
}
func (s *Server) conversationBlock(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	cid := pathID(r, "cid")
	on := r.Method != http.MethodDelete
	if e := s.st.SetConversationBlocked(r.Context(), cid, u.ID, on); s.readError(w, r, e) {
		return
	}
	s.respond(w, 200, map[string]bool{"blocked": on})
}

func (s *Server) conversationRead(w http.ResponseWriter, r *http.Request) {
	mid, err := strconv.ParseInt(r.PostFormValue("messageId"), 10, 64)
	if err != nil || mid <= 0 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "无效消息 ID")
		return
	}
	if !s.readError(w, r, s.st.ReadConversation(r.Context(), User(r).ID, pathID(r, "cid"), mid)) {
		s.respond(w, 200, map[string]bool{"read": true})
	}
}

func (s *Server) communityError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, store.ErrCommunityInvalid):
		s.fail(w, r, 422, "VALIDATION_FAILED", "字段或操作无效")
	case errors.Is(err, store.ErrCommunityConflict):
		s.fail(w, r, 409, "CONFLICT", "名称、别名冲突或版本已更新")
	case errors.Is(err, store.ErrMessageRateLimited):
		s.fail(w, r, 429, "MESSAGE_RATE_LIMITED", "发送过于频繁或新会话数量达到上限")
	case errors.Is(err, store.ErrMessageBlocked):
		s.fail(w, r, 403, "MESSAGE_BLOCKED", "已屏蔽，无法执行此操作")
	default:
		return s.readError(w, r, err)
	}
	return true
}
