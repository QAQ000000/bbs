// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/store"
)

// ---- 举报（ROADMAP 阶段三）----

// reportSubmit POST /report/{pid}：登录用户举报楼层（详情浮层表单，零 JS）。
func (s *Server) reportSubmit(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}
	if !s.requireLogin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allow(r, "report", 10, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "举报太频繁了，请稍后再试。")
		return
	}
	u := User(r)
	if !s.checkNotBanned(w, r) {
		return
	}
	pid := pathID(r, "pid")
	p, _ := s.visiblePost(w, r, pid)
	if p == nil {
		return
	}
	if p.AuthorID == u.ID {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "不能举报自己的内容")
		return

	}
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if utf8.RuneCountInString(reason) > 200 {
		reason = truncate(reason, 200)
	}
	if err := s.st.CreateReport(r.Context(), p.ID, u.ID, reason); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "举报失败", err.Error())
		return
	}
	result.Message = "举报已提交，感谢反馈，我们会尽快处理。"
	s.respond(w, http.StatusOK, result)
}

// adminReportHandle POST /admin/report/handle：处理举报（删除楼层 / 驳回）。
func (s *Server) adminReportHandle(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	rid := formInt64(r, "id")
	op := r.PostFormValue("op")
	if op != "delete" && op != "dismiss" {
		s.fail(w, r, 422, "VALIDATION_FAILED", "处理方式必须为 delete 或 dismiss")
		return
	}
	// Capture broadcast context before deletion; permissions are checked again in the transaction.
	tid, _ := s.st.ReportThreadID(r.Context(), rid)
	th, _ := s.st.Thread(r.Context(), tid)
	pid, tid, floor, alreadyDeleted, err := s.st.HandleReport(r.Context(), rid, User(r).ID, op, maskIP(remoteIP(r)))
	if errors.Is(err, store.ErrReportNotFound) {
		s.fail(w, r, http.StatusNotFound, "VALIDATION_FAILED", "举报不存在或已被处理")
		return

	} else if errors.Is(err, store.ErrReportForbidden) {
		s.fail(w, r, 403, "FORBIDDEN", "无权处理该版块的举报")
		return
	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if op == "delete" {
		if !alreadyDeleted && th != nil {
			s.broadcastPost("post.delete", th, &store.Post{ID: pid, ThreadID: tid, Floor: floor})
			s.broadcastThread("thread.update", th)
		}
		result.Message = "已删除被举报楼层"
		if alreadyDeleted {
			result.Message = "被举报内容已删除，举报已结案"
		}
	} else {
		result.Message = "举报已驳回"
	}
	s.respond(w, http.StatusOK, result)
}
