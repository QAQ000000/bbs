// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"dzforum/internal/perm"
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
	result := actionResult{}

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
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		if id == 0 {
			s.logOp(r, "forum.create", "新建版块 #"+strconv.FormatInt(fid, 10)+" "+name)
			result.Message = "版块已创建"
		} else {
			s.logOp(r, "forum.update", "编辑版块 #"+strconv.FormatInt(id, 10)+" "+name)
			result.Message = "版块已保存"
		}
	}
	s.respond(w, http.StatusOK, result)
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
	result := actionResult{}

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
		s.fail(w, r, http.StatusNotFound, "VALIDATION_FAILED", "版块不存在")
		return
	case err != nil:
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	default:
		s.logOp(r, "forum.delete", "删除版块 #"+strconv.FormatInt(id, 10))
		result.Message = "版块已删除"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminForumMove(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

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
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "forum.move", "调整版块排序 #"+strconv.FormatInt(id, 10))
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminCatSave(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

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
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "category.save", "保存分类 #"+strconv.Itoa(id)+" "+name)
		result.Message = "分类已保存"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminCatDelete(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	id := int(formInt64(r, "id"))
	if err := s.st.DeleteCategory(r.Context(), id); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "category.delete", "删除分类 #"+strconv.Itoa(id))
		result.Message = "分类已删除"
	}
	s.respond(w, http.StatusOK, result)
}

// ---- 内容管理操作 ----

func (s *Server) adminThreadAction(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

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
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请先勾选主题")
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
	if op == "move" {
		dest := formInt64(r, "move_to")
		if dest <= 0 {
			s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请先选择目标版块")
			return

		}
		scope := s.staffForumScope(r)
		moved := 0
		for _, tid := range tids {
			th, err := s.st.Thread(r.Context(), tid)
			if err != nil || !inForumScope(scope, th.ForumID) || !inForumScope(scope, dest) {
				continue // 版主只能在其管辖版块之间移动
			}
			oldFID, err := s.st.MoveThread(r.Context(), tid, dest)
			if err != nil {
				continue
			}
			moved++
			s.publish("f:"+strconv.FormatInt(oldFID, 10), eventBody{Type: "thread.delete", TID: tid})
			if th2, err := s.st.Thread(r.Context(), tid); err == nil {
				s.broadcastThread("thread.new", th2)
			}
		}
		s.logOp(r, "thread.move", "移动主题 ×"+strconv.Itoa(moved)+" → 版块 #"+strconv.FormatInt(dest, 10))
		result.Message = "已移动 " + strconv.Itoa(moved) + " 个主题"
		s.respond(w, http.StatusOK, result)
		return
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
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "thread."+op, "主题 "+op+" ×"+strconv.Itoa(len(tids))+"："+trimTIDs(tids))
		result.Message = "已对 " + strconv.Itoa(len(affected)) + " 个主题执行操作"
	}
	s.respond(w, http.StatusOK, result)
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
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	// 细粒度权限点即时生效：矩阵撤销后即使仍能进后台也不能操作
	if !hasPoint(User(r), perm.UserBan) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "你没有执行该操作的权限。")
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
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
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
		result.Message = "已禁言"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminUserUnban(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	// 细粒度权限点即时生效：矩阵撤销后即使仍能进后台也不能操作
	if !hasPoint(User(r), perm.UserBan) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "你没有执行该操作的权限。")
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	if err := s.st.UnbanUser(r.Context(), uid); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "user.unban", "解除禁言 #"+strconv.FormatInt(uid, 10))
		result.Message = "已解禁"
	}
	s.respond(w, http.StatusOK, result)
}

// adminUserBlock 封禁（禁止登录，区别于禁言）：踢掉全部会话。
func (s *Server) adminUserBlock(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	if u := User(r); u != nil && u.ID == uid {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "不能封禁自己的账号")
		return

	}
	days := int(formInt64(r, "days"))
	if err := s.st.BlockUser(r.Context(), uid, days); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		detail := "封禁用户 #" + strconv.FormatInt(uid, 10)
		if days > 0 {
			detail += " " + strconv.Itoa(days) + " 天"
		} else {
			detail += " 永久"
		}
		s.logOp(r, "user.block", detail)
		result.Message = "已封禁（禁止登录）"
	}
	s.respond(w, http.StatusOK, result)
}

// adminUserUnblock 解除封禁。
func (s *Server) adminUserUnblock(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	if err := s.st.UnblockUser(r.Context(), uid); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		s.logOp(r, "user.unblock", "解除封禁 #"+strconv.FormatInt(uid, 10))
		result.Message = "已解封"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminUserGroup(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	// 细粒度权限点即时生效：矩阵撤销后即使仍能进后台也不能操作
	if !hasPoint(User(r), perm.UserSetGroup) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "你没有执行该操作的权限。")
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	group := int(formInt64(r, "group"))
	if group < 0 || group > 2 {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "未知用户组")
		return

	}
	if u := User(r); u != nil && u.ID == uid && group != 1 {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "不能降级自己的管理员身份")
		return

	}
	if err := s.st.SetUserGroup(r.Context(), uid, group); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return
	} else {
		name := map[int]string{0: "会员", 1: "管理员", 2: "版主"}[group]
		s.logOp(r, "user.group", "调整用户组 #"+strconv.FormatInt(uid, 10)+" → "+name)
		result.Message = "用户组已调整"
	}
	s.respond(w, http.StatusOK, result)
}

func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireAdmin(w, r) {
		return
	}
	// 细粒度权限点即时生效：矩阵撤销后即使仍能进后台也不能操作
	if !hasPoint(User(r), perm.UserDelete) {
		s.fail(w, r, http.StatusForbidden, "无权操作", "你没有执行该操作的权限。")
		return
	}
	if !s.checkCSRF(r) {
		s.forbidden(w, r)
		return
	}
	uid := formInt64(r, "uid")
	if u := User(r); u != nil && u.ID == uid {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "不能删除自己的账号")
		return

	}
	err := s.st.DeleteUser(r.Context(), uid)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, r, http.StatusNotFound, "VALIDATION_FAILED", "用户不存在")
		return
	case errors.Is(err, store.ErrUserHasContent):
		s.fail(w, r, http.StatusConflict, "USER_HAS_CONTENT", "账号仍有公开内容")
		return
	case err != nil:
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "删号失败，请稍后重试")
		return
	default:
		s.logOp(r, "user.delete", "删除用户 #"+strconv.FormatInt(uid, 10))
		result.Message = "用户已删除"
	}
	s.respond(w, http.StatusOK, result)
}

// ---- 站点设置保存 ----

func (s *Server) adminSettingsSave(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

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
	if r.PostFormValue("register_enabled") != "1" {
		reg = "0"
	}
	moderate := "1"
	if r.PostFormValue("moderate_enabled") != "1" {
		moderate = "0"
	}
	upload := "1"
	if r.PostFormValue("upload_enabled") != "1" {
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
	consent := "0"
	if r.PostFormValue("require_consent") == "1" {
		consent = "1"
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
		"upload_max_disk_gb":   strings.TrimSpace(r.PostFormValue("upload_max_disk_gb")),
		"captcha_enabled":      captcha,
		"email_verify_enabled": emailVerify,
		"require_consent":      consent,
		"site_logo":            strings.TrimSpace(r.PostFormValue("site_logo")),
		"footer_text":          strings.TrimSpace(r.PostFormValue("footer_text")),
		"terms_content":        r.PostFormValue("terms_content"),
		"privacy_content":      r.PostFormValue("privacy_content"),
		"site_closed":          closed,
		"site_closed_reason":   strings.TrimSpace(r.PostFormValue("site_closed_reason")),
	}
	if err := s.st.SaveSettings(r.Context(), kv); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "操作失败", err.Error())
		return

	}
	s.logOp(r, "settings.save", "更新站点设置")
	result.Message = "设置已保存"
	s.respond(w, http.StatusOK, result)
}

func (s *Server) forbidden(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新后重试。")
}
