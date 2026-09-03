// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"net/http"
	"strconv"

	"dzforum/internal/store"
)

// ---- 回收站 ----

func (s *Server) adminRecycle(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	threads, total, err := s.st.RecycleThreads(r.Context(), page, 20)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	totalPage := (total + 19) / 20
	if totalPage < 1 {
		totalPage = 1
	}
	data := struct {
		Common
		Threads   []*store.Thread
		Page      []PageItem
		PageNum   int
		TotalPage int
	}{s.adminCommon(r, "recycle"), threads, nil, page, totalPage}
	data.Page = BuildPage(page, totalPage, func(n int) string {
		return "/admin/recyclebin?page=" + strconv.Itoa(n)
	})
	_ = s.rd.Render(w, "admin_recycle.html", &data)
}

func (s *Server) adminRecycleRestore(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	tid := formInt64(r, "tid")
	if err := s.st.RestoreThread(r.Context(), tid); err != nil {
		s.setFlash(w, "恢复失败：" + err.Error())
	} else {
		if th, err := s.st.Thread(r.Context(), tid); err == nil {
			s.broadcastThread("thread.new", th)
		}
		s.logOp(r, "recycle.restore", "恢复主题 #"+strconv.FormatInt(tid, 10))
		s.setFlash(w, "主题已恢复")
	}
	http.Redirect(w, r, "/admin/recyclebin", http.StatusSeeOther)
}

func (s *Server) adminRecyclePurge(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	tid := formInt64(r, "tid")
	if err := s.st.PurgeThread(r.Context(), tid); err != nil {
		s.setFlash(w, "删除失败：" + err.Error())
	} else {
		s.logOp(r, "recycle.purge", "彻底删除主题 #"+strconv.FormatInt(tid, 10))
		s.setFlash(w, "主题已彻底删除")
	}
	http.Redirect(w, r, "/admin/recyclebin", http.StatusSeeOther)
}

func (s *Server) adminRecyclePurgeAll(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	n, err := s.st.PurgeRecycle(r.Context())
	if err != nil {
		s.setFlash(w, "清空失败：" + err.Error())
	} else {
		s.logOp(r, "recycle.purgeAll", "清空回收站，共 "+strconv.FormatInt(n, 10)+" 个主题")
		s.setFlash(w, "已清空 " + strconv.FormatInt(n, 10) + " 个主题")
	}
	http.Redirect(w, r, "/admin/recyclebin", http.StatusSeeOther)
}

// ---- 敏感词 ----

func (s *Server) adminCensor(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	words := s.st.CensorWords(r.Context())
	if words == nil {
		words = []store.CensorWord{}
	}
	data := struct {
		Common
		Words []store.CensorWord
	}{s.adminCommon(r, "censor"), words}
	_ = s.rd.Render(w, "admin_censor.html", &data)
}

func (s *Server) adminCensorAdd(w http.ResponseWriter, r *http.Request) {
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
		s.setFlash(w, "添加失败：" + err.Error())
	} else {
		s.logOp(r, "censor.add", "敏感词："+word+" → "+displayRepl(repl))
		s.setFlash(w, "敏感词已添加")
	}
	http.Redirect(w, r, "/admin/censor", http.StatusSeeOther)
}

func (s *Server) adminCensorDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	if err := s.st.DeleteCensorWord(r.Context(), id); err != nil {
		s.setFlash(w, "删除失败：" + err.Error())
	} else {
		s.logOp(r, "censor.delete", "删除敏感词 #"+strconv.FormatInt(id, 10))
		s.setFlash(w, "敏感词已删除")
	}
	http.Redirect(w, r, "/admin/censor", http.StatusSeeOther)
}

func displayRepl(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

// ---- 公告 ----

func (s *Server) adminAnnounce(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	list, err := s.st.Announcements(r.Context(), false, 50)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if list == nil {
		list = []*store.Announcement{}
	}
	data := struct {
		Common
		Announcements []*store.Announcement
	}{s.adminCommon(r, "announce"), list}
	_ = s.rd.Render(w, "admin_announce.html", &data)
}

func (s *Server) adminAnnounceAdd(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	u := User(r)
	if err := s.st.SaveAnnouncement(r.Context(), u.ID, u.Username, r.PostFormValue("content")); err != nil {
		s.setFlash(w, "发布失败：" + err.Error())
	} else {
		s.logOp(r, "announce.add", "发布公告："+truncate(r.PostFormValue("content"), 50))
		s.setFlash(w, "公告已发布")
	}
	http.Redirect(w, r, "/admin/announcements", http.StatusSeeOther)
}

func (s *Server) adminAnnounceToggle(w http.ResponseWriter, r *http.Request) {
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
		s.setFlash(w, "操作失败：" + err.Error())
	} else {
		state := "停用"
		if enabled {
			state = "启用"
		}
		s.logOp(r, "announce.toggle", state+"公告 #"+strconv.FormatInt(id, 10))
		s.setFlash(w, "公告已"+state)
	}
	http.Redirect(w, r, "/admin/announcements", http.StatusSeeOther)
}

func (s *Server) adminAnnounceDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	if err := s.st.DeleteAnnouncement(r.Context(), id); err != nil {
		s.setFlash(w, "删除失败：" + err.Error())
	} else {
		s.logOp(r, "announce.delete", "删除公告 #"+strconv.FormatInt(id, 10))
		s.setFlash(w, "公告已删除")
	}
	http.Redirect(w, r, "/admin/announcements", http.StatusSeeOther)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
