// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"dzforum/internal/store"
)

// logOp 记录后台操作日志。
func (s *Server) logOp(r *http.Request, action, detail string) {
	if u := User(r); u != nil {
		ip := strings.Split(r.RemoteAddr, ":")[0]
		s.st.AdminLog(r.Context(), u.ID, u.Username, action, detail, ip)
	}
}

// formInt64 表单整型参数。
func formInt64(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(r.PostFormValue(key), 10, 64)
	return n
}

// ---- 版块管理操作 ----

func (s *Server) adminForumSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	cat := int(formInt64(r, "category_id"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	desc := strings.TrimSpace(r.PostFormValue("description"))
	mods := strings.Join(splitModerators(r.PostFormValue("moderators")), ",")
	fid, err := s.st.SaveForum(r.Context(), id, cat, name, desc, mods)
	if err != nil {
		s.setFlash(w, "保存失败："+err.Error())
	} else {
		if id == 0 {
			s.logOp(r, "forum.create", "新建版块 #"+strconv.FormatInt(fid, 10)+" "+name)
			s.setFlash(w, "版块已创建")
		} else {
			s.logOp(r, "forum.update", "编辑版块 #"+strconv.FormatInt(id, 10)+" "+name)
			s.setFlash(w, "版块已保存")
		}
	}
	http.Redirect(w, r, "/admin/forums", http.StatusSeeOther)
}

// splitModerators 拆分版主用户名列表（中英文逗号、空格分隔）。
func splitModerators(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\t'
	}) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (s *Server) adminForumDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	err := s.st.DeleteForum(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.setFlash(w, "版块不存在")
	case err != nil:
		s.setFlash(w, "删除失败："+err.Error())
	default:
		s.logOp(r, "forum.delete", "删除版块 #"+strconv.FormatInt(id, 10))
		s.setFlash(w, "版块已删除")
	}
	http.Redirect(w, r, "/admin/forums", http.StatusSeeOther)
}

func (s *Server) adminForumMove(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := formInt64(r, "id")
	up := r.PostFormValue("dir") == "up"
	if err := s.st.MoveForum(r.Context(), id, up); err != nil {
		s.setFlash(w, "移动失败："+err.Error())
	} else {
		s.logOp(r, "forum.move", "调整版块排序 #"+strconv.FormatInt(id, 10))
	}
	http.Redirect(w, r, "/admin/forums", http.StatusSeeOther)
}

func (s *Server) adminCatSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := int(formInt64(r, "id"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	if err := s.st.SaveCategory(r.Context(), id, name); err != nil {
		s.setFlash(w, "保存失败："+err.Error())
	} else {
		s.logOp(r, "category.save", "保存分类 #"+strconv.Itoa(id)+" "+name)
		s.setFlash(w, "分类已保存")
	}
	http.Redirect(w, r, "/admin/forums", http.StatusSeeOther)
}

func (s *Server) adminCatDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := int(formInt64(r, "id"))
	if err := s.st.DeleteCategory(r.Context(), id); err != nil {
		s.setFlash(w, "删除失败："+err.Error())
	} else {
		s.logOp(r, "category.delete", "删除分类 #"+strconv.Itoa(id))
		s.setFlash(w, "分类已删除")
	}
	http.Redirect(w, r, "/admin/forums", http.StatusSeeOther)
}

// ---- 内容管理操作 ----

func (s *Server) adminThreadAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireStaff(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	op := r.PostFormValue("op")
	var tids []int64
	for _, v := range r.Form["tid"] {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			tids = append(tids, n)
		}
	}
	if len(tids) == 0 {
		s.setFlash(w, "请先勾选主题")
		http.Redirect(w, r, "/admin/threads", http.StatusSeeOther)
		return
	}
	// 版主只能操作其管辖版块的主题
	scope := s.staffForumScope(r)
	if len(scope) > 0 {
		scopeSet := map[int64]bool{}
		for _, id := range scope {
			scopeSet[id] = true
		}
		var allowed []int64
		for _, tid := range tids {
			if th, err := s.st.Thread(r.Context(), tid); err == nil && scopeSet[th.ForumID] {
				allowed = append(allowed, tid)
			}
		}
		tids = allowed
	}
	affected, err := s.st.AdminThreadAction(r.Context(), op, tids)
	for _, th := range affected {
		if op == "delete" {
			s.publish("t:"+strconv.FormatInt(th.ID, 10), eventBody{Type: "thread.deleted", TID: th.ID})
			s.broadcastThread("thread.delete", th)
		} else {
			s.broadcastThread("thread.update", th)
		}
	}
	if err != nil {
		s.setFlash(w, "部分操作失败："+err.Error())
	} else {
		s.logOp(r, "thread."+op, "主题 "+op+" ×"+strconv.Itoa(len(tids))+"："+trimTIDs(tids))
		s.setFlash(w, "已对 "+strconv.Itoa(len(affected))+" 个主题执行操作")
	}
	http.Redirect(w, r, r.PostFormValue("back"), http.StatusSeeOther)
}

func trimTIDs(tids []int64) string {
	var sb strings.Builder
	for i, t := range tids {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(strconv.FormatInt(t, 10))
		if sb.Len() > 100 {
			sb.WriteString("…")
			break
		}
	}
	return sb.String()
}

// ---- 用户管理操作 ----

func (s *Server) adminUserBan(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	days := int(formInt64(r, "days"))
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if err := s.st.BanUser(r.Context(), uid, days, reason); err != nil {
		s.setFlash(w, "禁言失败："+err.Error())
	} else {
		detail := "禁言用户 #" + strconv.FormatInt(uid, 10)
		if days > 0 {
			detail += " " + strconv.Itoa(days) + " 天"
		} else {
			detail += " 永久"
		}
		if reason != "" {
			detail += "，理由：" + reason
		}
		s.logOp(r, "user.ban", detail)
		s.setFlash(w, "已禁言")
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) adminUserUnban(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	if err := s.st.UnbanUser(r.Context(), uid); err != nil {
		s.setFlash(w, "解禁失败："+err.Error())
	} else {
		s.logOp(r, "user.unban", "解除禁言 #"+strconv.FormatInt(uid, 10))
		s.setFlash(w, "已解禁")
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) adminUserGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	group := int(formInt64(r, "group"))
	if group < 0 || group > 2 {
		s.setFlash(w, "未知用户组")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if u := User(r); u != nil && u.ID == uid && group != 1 {
		s.setFlash(w, "不能降级自己的管理员身份")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if err := s.st.SetUserGroup(r.Context(), uid, group); err != nil {
		s.setFlash(w, "修改失败："+err.Error())
	} else {
		name := map[int]string{0: "会员", 1: "管理员", 2: "版主"}[group]
		s.logOp(r, "user.group", "调整用户组 #"+strconv.FormatInt(uid, 10)+" → "+name)
		s.setFlash(w, "用户组已调整")
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	if u := User(r); u != nil && u.ID == uid {
		s.setFlash(w, "不能删除自己的账号")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	err := s.st.DeleteUser(r.Context(), uid)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.setFlash(w, "用户不存在")
	case errors.Is(err, store.ErrUserHasContent):
		s.setFlash(w, err.Error())
	case err != nil:
		s.setFlash(w, "删号失败，请稍后重试")
	default:
		s.logOp(r, "user.delete", "删除用户 #"+strconv.FormatInt(uid, 10))
		s.setFlash(w, "用户已删除")
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// ---- 站点设置保存 ----

func (s *Server) adminSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	closed := "0"
	if r.PostFormValue("site_closed") == "1" {
		closed = "1"
	}
	reg := "1"
	if r.PostFormValue("register_enabled") == "" {
		reg = "0"
	}
	moderate := "1"
	if r.PostFormValue("moderate_enabled") == "" {
		moderate = "0"
	}
	upload := "1"
	if r.PostFormValue("upload_enabled") == "" {
		upload = "0"
	}
	captcha := "0"
	if r.PostFormValue("captcha_enabled") == "1" {
		captcha = "1"
	}
	emailVerify := "0"
	if r.PostFormValue("email_verify_enabled") == "1" {
		emailVerify = "1"
	}
	kv := map[string]string{
		"site_name":            strings.TrimSpace(r.PostFormValue("site_name")),
		"threads_per_page":     strings.TrimSpace(r.PostFormValue("threads_per_page")),
		"posts_per_page":       strings.TrimSpace(r.PostFormValue("posts_per_page")),
		"register_enabled":     reg,
		"moderate_enabled":     moderate,
		"upload_enabled":       upload,
		"max_image_mb":         strings.TrimSpace(r.PostFormValue("max_image_mb")),
		"max_file_mb":          strings.TrimSpace(r.PostFormValue("max_file_mb")),
		"captcha_enabled":      captcha,
		"email_verify_enabled": emailVerify,
		"site_closed":          closed,
		"site_closed_reason":   strings.TrimSpace(r.PostFormValue("site_closed_reason")),
	}
	if err := s.st.SaveSettings(r.Context(), kv); err != nil {
		s.setFlash(w, "保存失败："+err.Error())
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}
	s.logOp(r, "settings.save", "更新站点设置")
	s.setFlash(w, "设置已保存")
	http.Redirect(w, r, "/admin/settings?saved=1", http.StatusSeeOther)
}

func (s *Server) forbidden(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新后重试。")
}
