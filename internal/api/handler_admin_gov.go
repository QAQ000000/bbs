// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"net/http"
	"strconv"

	"dzforum/internal/perm"
)

// ---- 回收站 ----

func (s *Server) adminRecycleRestore(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireStaffPoint(w, r, perm.RecycleBin) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	tid := formInt64(r, "tid")
	if !s.canModerateRecycleThread(r, tid) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "只能恢复自己管辖版块的内容。")
		return
	}
	if err := s.st.RestoreThread(r.Context(), tid); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		if th, err := s.st.Thread(r.Context(), tid); err == nil {
			s.broadcastThread("thread.new", th)
		}
		s.logOp(r, "recycle.restore", "恢复主题 #"+strconv.FormatInt(tid, 10))
		result.Message = "主题已恢复"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminRecyclePurge(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireStaffPoint(w, r, perm.RecycleBin) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	tid := formInt64(r, "tid")
	if !s.canModerateRecycleThread(r, tid) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "只能彻底删除自己管辖版块的内容。")
		return
	}
	if err := s.st.PurgeThread(r.Context(), tid); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "recycle.purge", "彻底删除主题 #"+strconv.FormatInt(tid, 10))
		result.Message = "主题已彻底删除"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminRecyclePurgeAll(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireStaffPoint(w, r, perm.RecycleBin) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	n, err := s.st.PurgeRecycle(r.Context(), s.staffForumScope(r))
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "recycle.purgeAll", "清空回收站，共 "+strconv.FormatInt(n, 10)+" 个主题")
		result.Message = "已清空 " + strconv.FormatInt(n, 10) + " 个主题"
	}
	s.respond(w, http.StatusOK, result)
}

// ---- 敏感词 ----

func (s *Server) adminCensorAdd(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	word := r.PostFormValue("word")
	repl := r.PostFormValue("replacement")
	if err := s.st.AddCensorWord(r.Context(), word, repl); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "censor.add", "敏感词："+word+" → "+displayRepl(repl))
		result.Message = "敏感词已添加"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminCensorDelete(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	if err := s.st.DeleteCensorWord(r.Context(), id); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "censor.delete", "删除敏感词 #"+strconv.FormatInt(id, 10))
		result.Message = "敏感词已删除"
	}
	s.respond(w, http.StatusOK, result)
}

func displayRepl(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

// ---- 公告 ----

func (s *Server) adminAnnounceAdd(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	u := User(r)
	if err := s.st.SaveAnnouncement(r.Context(), u.ID, u.Username, r.PostFormValue("content")); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "announce.add", "发布公告："+truncate(r.PostFormValue("content"), 50))
		result.Message = "公告已发布"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminAnnounceToggle(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	enabled := r.PostFormValue("enabled") == "1"
	if err := s.st.SetAnnouncementEnabled(r.Context(), id, enabled); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		state := "停用"
		if enabled {
			state = "启用"
		}
		s.logOp(r, "announce.toggle", state+"公告 #"+strconv.FormatInt(id, 10))
		result.Message = "公告已" + state
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminAnnounceDelete(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	if err := s.st.DeleteAnnouncement(r.Context(), id); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "announce.delete", "删除公告 #"+strconv.FormatInt(id, 10))
		result.Message = "公告已删除"
	}
	s.respond(w, http.StatusOK, result)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// canModerateRecycleThread 回收站操作校验：主题须已软删且在管辖范围内。
func (s *Server) canModerateRecycleThread(r *http.Request, tid int64) bool {
	fid, err := s.st.DeletedThreadForumID(r.Context(), tid)
	if err != nil {
		return false
	}
	return inForumScope(s.staffForumScope(r), fid)
}
