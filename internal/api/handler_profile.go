// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// ---- 个人设置（ROADMAP 阶段一）：签名/邮箱资料 + 改密（撤销其他会话）----

// profileSave POST /profile/save：签名 + 邮箱。
func (s *Server) profileSave(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allow(r, "profile-save", 10, time.Minute) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	signature := strings.TrimSpace(r.PostFormValue("signature"))
	email := strings.TrimSpace(r.PostFormValue("email"))

	fail := func(msg string) { s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg) }
	if utf8.RuneCountInString(signature) > 200 || !memberSignatureAllowed(r, signature) {
		fail("签名超过站点或等级允许的长度")
		return
	}
	if email != "" && !emailRe.MatchString(email) {
		fail("邮箱格式不正确")
		return
	}
	if email != "" {
		if other, err := s.st.UIDByEmail(r.Context(), email); err == nil && other != u.ID {
			fail("该邮箱已被其他账号使用")
			return
		}
	}
	emailChanged, err := s.st.UpdateProfile(r.Context(), u.ID, signature, email)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	if emailChanged {
		// 换绑邮箱即作废旧验证结论与旧验证链接，需对新邮箱重新验证
		result.Message = "资料已保存；邮箱已变更，请重新验证新邮箱"
	} else {
		result.Message = "资料已保存"
	}
	s.respond(w, http.StatusOK, result)
}

// profilePassword POST /profile/password：旧密码验证 + 撤销其他会话。
func (s *Server) profilePassword(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	// 旧密码验证是唯一闸门：限流防在线爆破
	if !s.allow(r, "profile-pw", 10, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "密码修改尝试过多，请一小时后再试。")
		return
	}
	old := r.PostFormValue("old_password")
	new := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	fail := func(msg string) { s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", msg) }
	if utf8.RuneCountInString(new) < 8 {
		fail("新密码至少 8 位")
		return
	}
	if new != confirm {
		fail("两次输入的新密码不一致")
		return
	}
	if new == old {
		fail("新密码不能与当前密码相同")
		return
	}
	// Cookie 里是原始 token；留空则撤销全部会话（防御分支，正常必非空）
	raw := ""
	if c, err := r.Cookie(cookieSession); err == nil {
		raw = c.Value
	}
	if err := s.st.ChangePassword(r.Context(), u.ID, old, new, raw); err != nil {
		if errors.Is(err, store.ErrWrongPassword) {
			fail("当前密码不正确")
			return
		}
		s.fail(w, r, http.StatusInternalServerError, "修改失败", err.Error())
		return
	}
	result.Message = "密码已修改，其他设备已退出登录"
	s.respond(w, http.StatusOK, result)
}

// profileVerifyResend POST /profile/verify-resend：重发验证邮件（3 次/小时）。
func (s *Server) profileVerifyResend(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.emailGateEnabled() || u.Email == "" || u.EmailVerified {
		s.respond(w, http.StatusOK, result)
		return
	}
	if !s.allowKey("verify:"+strconv.FormatInt(u.ID, 10), 3, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "验证邮件发送过于频繁，请一小时后再试。")
		return
	}
	if err := s.st.QueueAuthEmail(r.Context(), u.ID, u.Email, "email_verify", s.mailTokens.Seal); err != nil {
		s.fail(w, r, 503, "EMAIL_QUEUE_FAILED", "验证邮件暂时无法加入队列，请稍后重试")
		return
	}
	result.Message = "验证邮件已加入发送队列（24 小时内有效）"
	s.respond(w, http.StatusOK, result)
}

// profileAvatar POST /profile/avatar：上传自定义头像（≤2MB，按内容嗅探）。
// 存储约定 data/uploads/avatars/uid.<ext>；/avatar/{uid} 自动优先展示。
func (s *Server) profileAvatar(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	// 请求体上限必须在任何表单读取（含 CSRF 的 PostFormValue）之前设置：
	// 表单解析会触发 multipart 读取，之后再限制对已读内容无效
	r.Body = http.MaxBytesReader(w, r.Body, (2<<20)+(64<<10))
	if !s.allowKey("avatar:"+strconv.FormatInt(u.ID, 10), 5, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "头像更换太频繁，请稍后再试。")
		return
	}
	// 请求体已限长（约 2.06MB），先解析再做 CSRF 校验：
	// 超限请求在此即被拒绝（413），不会走到任何写路径
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		s.fail(w, r, http.StatusRequestEntityTooLarge, "头像过大", "头像图片不能超过 2MB。")
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	// 头像计入站点上传占用，磁盘达上限时同样拒绝
	if gb := s.sets(r).UploadMaxDiskGB; gb > 0 && s.uploadDirBytes() >= int64(gb)<<30 {
		s.fail(w, r, http.StatusInsufficientStorage, "存储已达上限",
			"站点存储已达上限，暂时无法上传，请联系管理员。")
		return
	}
	f, _, err := r.FormFile("avatar")
	if err != nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请选择头像图片")
		return

	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	mime := http.DetectContentType(buf[:n])
	if i := strings.Index(mime, ";"); i > 0 {
		mime = mime[:i]
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[mime]
	if ext == "" {
		s.fail(w, r, http.StatusUnsupportedMediaType, "格式不支持", "头像仅支持 JPG/PNG/GIF/WebP 图片。")
		return
	}
	// 嗅探读取了前 512 字节，必须回到起点再复制，否则文件丢头（小图变空文件）
	if _, err := f.Seek(0, 0); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	dir := filepath.Join(s.cfg.UploadDir, "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	// 先写临时文件、完整落盘后再原子替换：写一半失败不丢旧头像
	tmp := filepath.Join(dir, strconv.FormatInt(u.ID, 10)+ext+".part")
	dst := filepath.Join(dir, strconv.FormatInt(u.ID, 10)+ext)
	out, err := os.Create(tmp)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	written, err := io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && written == 0 {
		err = errors.New("空文件")
	}
	if err != nil {
		_ = os.Remove(tmp)
		s.fail(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	if old := s.customAvatarPath(u.ID); old != "" && old != dst {
		_ = os.Remove(old) // 清掉旧扩展名头像
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		s.fail(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	s.logOp(r, "profile.avatar", "更新自定义头像")
	result.Message = "头像已更新"
	s.respond(w, http.StatusOK, result)
}

// profileAvatarClear POST /profile/avatar/clear：恢复默认字母头像。
func (s *Server) profileAvatarClear(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if p := s.customAvatarPath(u.ID); p != "" {
		_ = os.Remove(p)
	}
	result.Message = "已恢复默认头像"
	s.respond(w, http.StatusOK, result)
}

// profileSelfDelete POST /profile/delete：自助删号（需密码确认；
// 仍有公开内容时拒绝——与后台删号同口径）。
func (s *Server) profileSelfDelete(w http.ResponseWriter, r *http.Request) {
	result := actionResult{}

	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allow(r, "self-delete", 3, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "请一小时后再试。")
		return
	}
	if s.st.VerifyPassword(u, r.PostFormValue("password")) == false {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "密码不正确，账号未删除")
		return

	}
	if hasPoint(u, perm.AdminPanel) {
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "管理员账号不能自助删除，请先移交后台权限")
		return

	}
	err := s.st.DeleteUser(r.Context(), u.ID)
	switch {
	case errors.Is(err, store.ErrUserHasContent):
		s.fail(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "账号仍有发帖、私信会话、标签管理或积分流水记录，无法自助删除；请联系站长处理。")
		return

	case err != nil:
		s.fail(w, r, http.StatusInternalServerError, "删除失败", "请稍后重试或联系站长。")
		return
	}
	s.logOp(r, "profile.self_delete", "用户自助删号 "+u.Username)
	if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
		s.st.DeleteSession(r.Context(), c.Value)
	}
	s.setSessionCookie(w, "", -1)
	result.Message = "账号已注销"
	s.respond(w, http.StatusOK, result)
}

// profileExport GET /profile/export：导出本人数据（JSON 附件，审计留痕）。
func (s *Server) profileExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.allow(r, "export", 5, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "操作过于频繁", "导出过于频繁，请一小时后再试。")
		return
	}
	u := User(r)
	posts, err := s.st.ExportPostsOfUser(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "导出失败", err.Error())
		return
	}
	threads, err := s.st.ExportThreadsOfUser(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "导出失败", err.Error())
		return
	}
	s.logOp(r, "profile.export",
		"导出个人数据（threads="+strconv.Itoa(len(threads))+" posts="+strconv.Itoa(len(posts))+"）")
	membership, err := s.st.Membership(r.Context(), u.ID)
	if s.readError(w, r, err) {
		return
	}
	titles, err := s.st.UserTitles(r.Context(), u.ID)
	if s.readError(w, r, err) {
		return
	}
	b, err := json.MarshalIndent(map[string]any{
		"titles":     titles,
		"membership": membership,
		"account": map[string]any{
			"username":       u.Username,
			"email":          u.Email,
			"signature":      u.Signature,
			"post_count":     u.PostCount,
			"created_at":     u.CreatedAt,
			"email_verified": u.EmailVerified,
		},
		"threads":     threads,
		"posts":       posts,
		"exported_at": time.Now().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "导出失败", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="gobbs-export-`+strconv.FormatInt(u.ID, 10)+`.json"`)
	_, _ = w.Write(b)
}
