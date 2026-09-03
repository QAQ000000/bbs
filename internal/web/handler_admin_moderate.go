// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dzforum/internal/store"
)

// ---- 后台权限（阶段三）：版主可进入内容类页面，范围限其版块 ----

// isStaff 管理员或版主。
func isStaff(u *store.User) bool { return u != nil && u.IsStaff() }

// requireStaff 内容管理入口守卫；版主会被限制在其管辖版块。
func (s *Server) requireStaff(w http.ResponseWriter, r *http.Request) bool {
	u := User(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+urlQueryEscape(r.URL.RequestURI()), http.StatusFound)
		return false
	}
	if !u.IsStaff() {
		s.renderError(w, r, http.StatusForbidden, "无权访问", "该区域仅管理员与版主可访问。")
		return false
	}
	return true
}

// staffForumScope 当前管理人员可管理的版块范围；管理员为空（不限）。
func (s *Server) staffForumScope(r *http.Request) []int64 {
	u := User(r)
	if u == nil || u.IsAdmin() {
		return nil
	}
	ids, err := s.st.ModeratorForumIDs(r.Context(), u.Username)
	if err != nil {
		return []int64{0} // 出错时限定为不可能命中的版块，保守处理
	}
	return ids
}

// canModerateThread 管理人员是否可对某主题执行操作。
func (s *Server) canModerateThread(r *http.Request, th *store.Thread) bool {
	u := User(r)
	if u == nil {
		return false
	}
	if u.IsAdmin() {
		return true
	}
	ids, err := s.st.ModeratorForumIDs(r.Context(), u.Username)
	if err != nil {
		return false
	}
	for _, id := range ids {
		if id == th.ForumID {
			return true
		}
	}
	return false
}

// ---- 审核队列 ----

func (s *Server) adminModerate(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	threads, err := s.st.PendingThreads(r.Context(), 50)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	posts, err := s.st.PendingPosts(r.Context(), 50)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if threads == nil {
		threads = []*store.PendingThreadRow{}
	}
	if posts == nil {
		posts = []*store.PendingPostRow{}
	}
	data := struct {
		Common
		Threads []*store.PendingThreadRow
		Posts   []*store.PendingPostRow
	}{s.adminCommon(r, "moderate"), threads, posts}
	_ = s.rd.Render(w, "admin_moderate.html", &data)
}

func (s *Server) adminModerateThread(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	tid := formInt64(r, "tid")
	th, err := s.st.Thread(r.Context(), tid)
	if errors.Is(err, store.ErrNotFound) {
		s.setFlash(w, "主题不存在")
		http.Redirect(w, r, "/admin/moderate", http.StatusSeeOther)
		return
	} else if err != nil || !s.canModerateThread(r, th) {
		s.renderError(w, r, http.StatusForbidden, "无权操作", "只能审核自己管辖版块的内容。")
		return
	}
	if r.PostFormValue("op") == "delete" {
		if _, _, err := s.st.DeletePost(r.Context(), th.FirstPostID); err != nil {
			s.setFlash(w, "删除失败："+err.Error())
		} else {
			s.publish("t:"+strconv.FormatInt(tid, 10), eventBody{Type: "thread.deleted", TID: tid})
			s.broadcastThread("thread.delete", th)
			s.logOp(r, "moderate.thread.delete", "审核不通过并删除主题 #"+strconv.FormatInt(tid, 10))
			s.setFlash(w, "主题已删除")
		}
	} else {
		if err := s.st.SetThreadApproved(r.Context(), tid); err != nil {
			s.setFlash(w, "操作失败："+err.Error())
		} else {
			s.broadcastThread("thread.new", th)
			s.logOp(r, "moderate.thread.approve", "审核通过主题 #"+strconv.FormatInt(tid, 10))
			s.setFlash(w, "主题已通过审核")
		}
	}
	http.Redirect(w, r, "/admin/moderate", http.StatusSeeOther)
}

func (s *Server) adminModeratePost(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	pid := formInt64(r, "pid")
	p, err := s.st.Post(r.Context(), pid)
	if errors.Is(err, store.ErrNotFound) {
		s.setFlash(w, "回复不存在")
		http.Redirect(w, r, "/admin/moderate", http.StatusSeeOther)
		return
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	th, err := s.st.Thread(r.Context(), p.ThreadID)
	if err != nil || !s.canModerateThread(r, th) {
		s.renderError(w, r, http.StatusForbidden, "无权操作", "只能审核自己管辖版块的内容。")
		return
	}
	if r.PostFormValue("op") == "delete" {
		if _, _, err := s.st.DeletePost(r.Context(), pid); err != nil {
			s.setFlash(w, "删除失败："+err.Error())
		} else {
			s.broadcastPost("post.delete", th, &store.Post{ID: pid, ThreadID: p.ThreadID, Floor: p.Floor})
			s.logOp(r, "moderate.post.delete", "审核不通过并删除回复 #"+strconv.FormatInt(pid, 10))
			s.setFlash(w, "回复已删除")
		}
	} else {
		if err := s.st.SetPostApproved(r.Context(), pid); err != nil {
			s.setFlash(w, "操作失败："+err.Error())
		} else {
			s.broadcastPost("post.new", th, p)
			s.logOp(r, "moderate.post.approve", "审核通过回复 #"+strconv.FormatInt(pid, 10))
			s.setFlash(w, "回复已通过审核")
		}
	}
	http.Redirect(w, r, "/admin/moderate", http.StatusSeeOther)
}

// ---- 批量删帖 ----

func (s *Server) adminPrune(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	cats, _ := s.st.CategoriesWithForums(r.Context())
	data := struct {
		Common
		Categories []*store.Category
	}{s.adminCommon(r, "prune"), cats}
	_ = s.rd.Render(w, "admin_prune.html", &data)
}

func (s *Server) adminPruneExecute(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	kind := r.PostFormValue("kind") // thread | post
	author := strings.TrimSpace(r.PostFormValue("author"))
	keyword := strings.TrimSpace(r.PostFormValue("keyword"))
	forumID, _ := strconv.ParseInt(r.PostFormValue("forum"), 10, 64)
	days, _ := strconv.Atoi(r.PostFormValue("days"))
	scope := s.staffForumScope(r)

	switch kind {
	case "thread":
		if author == "" && keyword == "" && forumID == 0 && days <= 0 {
			s.setFlash(w, "拒绝无条件批量删除，请至少填写一个条件")
			http.Redirect(w, r, "/admin/prune", http.StatusSeeOther)
			return
		}
		// 主题范围：条件查询（含待审核主题），且限制在管辖版块
		threads, _, err := s.st.SearchThreads(r.Context(), store.ThreadQuery{
			ForumID: forumID, Keyword: keyword, Author: author,
			Page: 1, Size: 500, OnlyForumIDs: scope, IncludePending: true,
		})
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "查询失败", err.Error())
			return
		}
		// 天数条件在内存中过滤（created_at）
		var tids []int64
		for _, t := range threads {
			if days > 0 && !t.CreatedAt.Before(timeBeforeDays(days)) {
				continue
			}
			tids = append(tids, t.ID)
		}
		affected, err := s.st.AdminThreadAction(r.Context(), "delete", tids)
		for _, th := range affected {
			s.publish("t:"+strconv.FormatInt(th.ID, 10), eventBody{Type: "thread.deleted", TID: th.ID})
			s.broadcastThread("thread.delete", th)
		}
		if err != nil {
			s.setFlash(w, "部分删除失败："+err.Error())
		}
		s.logOp(r, "prune.thread", "批量删除主题 "+strconv.Itoa(len(affected))+" 个（author="+author+" keyword="+keyword+" forum="+strconv.FormatInt(forumID, 10)+" days="+strconv.Itoa(days)+"）")
		s.setFlash(w, "已批量删除 " + strconv.Itoa(len(affected)) + " 个主题（可在回收站恢复）")

	case "post":
		if forumID > 0 && len(scope) > 0 {
			allowed := false
			for _, id := range scope {
				if id == forumID {
					allowed = true
					break
				}
			}
			if !allowed {
				s.renderError(w, r, http.StatusForbidden, "无权操作", "只能清理自己管辖版块的内容。")
				return
			}
		}
		n, err := s.st.PrunePosts(r.Context(), author, forumID, days)
		if err != nil {
			s.setFlash(w, "清理失败："+err.Error())
		} else {
			s.logOp(r, "prune.post", "批量删除回复 "+strconv.FormatInt(n, 10)+" 条（author="+author+" forum="+strconv.FormatInt(forumID, 10)+" days="+strconv.Itoa(days)+"）")
			s.setFlash(w, "已批量删除 " + strconv.FormatInt(n, 10) + " 条回复")
		}

	default:
		s.setFlash(w, "未知操作类型")
	}
	http.Redirect(w, r, "/admin/prune", http.StatusSeeOther)
}

func timeBeforeDays(days int) time.Time {
	return time.Now().AddDate(0, 0, -days)
}
