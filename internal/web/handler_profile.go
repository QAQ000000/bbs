// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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

type profilePage struct {
	Common
	Profile    *store.User
	Section    string // 出错表单："profile" | "password"，空则无错误
	Error      string
	Signature  string
	Email      string
	EmailGate  bool // 邮箱验证闸门生效中（显示验证提示）
	MustChange bool // 初始密码未改（-seed 账号强制改密）
}

func (s *Server) renderProfile(w http.ResponseWriter, r *http.Request, d profilePage) {
	d.Common = s.common(r)
	d.Title = "资料设置"
	d.EmailGate = s.emailGateEnabled()
	if u := User(r); u != nil && u.MustChangePassword {
		d.MustChange = true
	}
	_ = s.rd.Render(w, "page_profile.html", &d)
}

// profileForm GET /profile：资料与改密表单。
func (s *Server) profileForm(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	s.renderProfile(w, r, profilePage{
		Profile:   u,
		Signature: u.Signature,
		Email:     u.Email,
	})
}

// profileSave POST /profile/save：签名 + 邮箱。
func (s *Server) profileSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allow(r, "profile-save", 10, time.Minute) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请稍后再试。")
		return
	}
	signature := strings.TrimSpace(r.PostFormValue("signature"))
	email := strings.TrimSpace(r.PostFormValue("email"))

	fail := func(msg string) {
		s.renderProfile(w, r, profilePage{Profile: u, Section: "profile",
			Error: msg, Signature: signature, Email: email})
	}
	if utf8.RuneCountInString(signature) > 200 {
		fail("签名不能超过 200 字")
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
	if err := s.st.UpdateProfile(r.Context(), u.ID, signature, email); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "保存失败", err.Error())
		return
	}
	s.setFlash(w, "资料已保存")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profilePassword POST /profile/password：旧密码验证 + 撤销其他会话。
func (s *Server) profilePassword(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	// 旧密码验证是唯一闸门：限流防在线爆破
	if !s.allow(r, "profile-pw", 10, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "密码修改尝试过多，请一小时后再试。")
		return
	}
	old := r.PostFormValue("old_password")
	new := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	fail := func(msg string) {
		s.renderProfile(w, r, profilePage{Profile: u, Section: "password", Error: msg})
	}
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
		s.renderError(w, r, http.StatusInternalServerError, "修改失败", err.Error())
		return
	}
	s.setFlash(w, "密码已修改，其他设备已退出登录")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profileVerifyResend POST /profile/verify-resend：重发验证邮件（3 次/小时）。
func (s *Server) profileVerifyResend(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.emailGateEnabled() || u.Email == "" || u.EmailVerified {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	if !s.allowKey("verify:"+strconv.FormatInt(u.ID, 10), 3, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "验证邮件发送过于频繁，请一小时后再试。")
		return
	}
	if raw, err := s.st.CreateEmailVerify(r.Context(), u.ID); err == nil {
		link := s.cfg.SiteURL + "/verify?token=" + url.QueryEscape(raw)
		s.mailer.NotifyEmailVerify(u.Email, link)
	}
	s.setFlash(w, "验证邮件已发送，请查收（24 小时内有效）")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profileAvatar POST /profile/avatar：上传自定义头像（≤2MB，按内容嗅探）。
// 存储约定 data/uploads/avatars/uid.<ext>；/avatar/{uid} 自动优先展示。
func (s *Server) profileAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allowKey("avatar:"+strconv.FormatInt(u.ID, 10), 5, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "头像更换太频繁，请稍后再试。")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		s.renderError(w, r, http.StatusRequestEntityTooLarge, "头像过大", "头像图片不能超过 2MB。")
		return
	}
	f, _, err := r.FormFile("avatar")
	if err != nil {
		s.setFlash(w, "请选择头像图片")
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
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
		s.renderError(w, r, http.StatusUnsupportedMediaType, "格式不支持", "头像仅支持 JPG/PNG/GIF/WebP 图片。")
		return
	}
	dir := filepath.Join(s.cfg.UploadDir, "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	// 清掉旧头像（换扩展名也能命中）
	if old := s.customAvatarPath(u.ID); old != "" {
		_ = os.Remove(old)
	}
	dst := filepath.Join(dir, strconv.FormatInt(u.ID, 10)+ext)
	out, err := os.Create(dst)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, f); err != nil {
		_ = os.Remove(dst)
		s.renderError(w, r, http.StatusInternalServerError, "上传失败", err.Error())
		return
	}
	s.logOp(r, "profile.avatar", "更新自定义头像")
	s.setFlash(w, "头像已更新")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profileAvatarClear POST /profile/avatar/clear：恢复默认字母头像。
func (s *Server) profileAvatarClear(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if p := s.customAvatarPath(u.ID); p != "" {
		_ = os.Remove(p)
	}
	s.setFlash(w, "已恢复默认头像")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// profileSelfDelete POST /profile/delete：自助删号（需密码确认；
// 仍有公开内容时拒绝——与后台删号同口径）。
func (s *Server) profileSelfDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	if !s.checkCSRF(r) {
		s.renderError(w, r, http.StatusForbidden, "操作被拒绝", "表单已过期，请刷新重试。")
		return
	}
	if !s.allow(r, "self-delete", 3, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "请一小时后再试。")
		return
	}
	if s.st.VerifyPassword(u, r.PostFormValue("password")) == false {
		s.setFlash(w, "密码不正确，账号未删除")
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	if hasPoint(u, perm.AdminPanel) {
		s.setFlash(w, "管理员账号不能自助删除，请先移交后台权限")
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	err := s.st.DeleteUser(r.Context(), u.ID)
	switch {
	case errors.Is(err, store.ErrUserHasContent):
		s.setFlash(w, "账号下仍有发帖内容，无法自助删除；请联系站长处理。")
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	case err != nil:
		s.renderError(w, r, http.StatusInternalServerError, "删除失败", "请稍后重试或联系站长。")
		return
	}
	s.logOp(r, "profile.self_delete", "用户自助删号 "+u.Username)
	if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
		s.st.DeleteSession(r.Context(), c.Value)
	}
	s.setSessionCookie(w, "", -1)
	s.setFlash(w, "账号已注销")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// profileExport GET /profile/export：导出本人数据（JSON 附件，审计留痕）。
func (s *Server) profileExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	if !s.allow(r, "export", 5, time.Hour) {
		s.renderError(w, r, http.StatusTooManyRequests, "操作过于频繁", "导出过于频繁，请一小时后再试。")
		return
	}
	u := User(r)
	posts, err := s.st.ExportPostsOfUser(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "导出失败", err.Error())
		return
	}
	threads, err := s.st.ExportThreadsOfUser(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "导出失败", err.Error())
		return
	}
	s.logOp(r, "profile.export",
		"导出个人数据（threads="+strconv.Itoa(len(threads))+" posts="+strconv.Itoa(len(posts))+"）")
	b, err := json.MarshalIndent(map[string]any{
		"account": map[string]any{
			"username":       u.Username,
			"email":          u.Email,
			"signature":      u.Signature,
			"trust_level":    u.TrustLevel,
			"post_count":     u.PostCount,
			"created_at":     u.CreatedAt,
			"email_verified": u.EmailVerified,
		},
		"threads":     threads,
		"posts":       posts,
		"exported_at": time.Now().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "导出失败", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="gobbs-export-`+strconv.FormatInt(u.ID, 10)+`.json"`)
	_, _ = w.Write(b)
}
