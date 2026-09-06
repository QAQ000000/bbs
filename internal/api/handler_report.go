// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"errors"
	"net/http"
	"strconv"
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
	tid, err := s.st.ReportThreadID(r.Context(), rid)
	if errors.Is(err, store.ErrReportNotFound) {
		s.fail(w, r, http.StatusNotFound, "VALIDATION_FAILED", "举报不存在或已被处理")
		return

	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	th, err := s.st.Thread(r.Context(), tid)
	if err != nil || !s.canModerateThread(r, th) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "只能处理自己管辖版块的举报。")
		return
	}
	u := User(r)
	switch r.PostFormValue("op") {
	case "delete":
		pid, err := s.st.SetReportStatus(r.Context(), rid, u.ID, store.ReportResolved)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
			return

		}
		p, err := s.st.Post(r.Context(), pid)
		if err == nil {
			if _, _, err := s.st.DeletePost(r.Context(), pid); err == nil {
				s.broadcastPost("post.delete", th, &store.Post{ID: pid, ThreadID: tid, Floor: p.Floor})
				s.broadcastThread("thread.update", th)
			}
		}
		s.logOp(r, "report.delete", "举报 #"+strconv.FormatInt(rid, 10)+"：删除楼层 #"+strconv.FormatInt(pid, 10))
		result.Message = "已删除被举报楼层"
	default: // dismiss
		if _, err := s.st.SetReportStatus(r.Context(), rid, u.ID, store.ReportDismissed); err != nil {
			s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
			return
		} else {
			s.logOp(r, "report.dismiss", "驳回举报 #"+strconv.FormatInt(rid, 10))
			result.Message = "举报已驳回"
		}
	}
	s.respond(w, http.StatusOK, result)
}
