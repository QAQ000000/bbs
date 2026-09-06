// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dzforum/internal/avatar"
	"dzforum/internal/perm"
	"dzforum/internal/store"
)

// ---- 图片上传 ----

var allowedImageMime = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var allowedFileMime = map[string]string{
	"application/pdf":              ".pdf",
	"text/plain":                   ".txt",
	"application/zip":              ".zip",
	"application/x-zip-compressed": ".zip",
}

// POST /api/upload（multipart，字段 file）。
func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	u := User(r)
	if u == nil {
		s.fail(w, r, http.StatusUnauthorized, "", `{"error":"unauthorized"}`)
		return
	}
	sets := s.sets(r)
	maxMB := max(sets.MaxImageMB, sets.MaxFileMB)
	r.Body = http.MaxBytesReader(w, r.Body, (int64(maxMB)<<20)+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		s.fail(w, r, http.StatusRequestEntityTooLarge, "", `{"error":"invalid or oversized upload"}`)
		return
	}
	defer r.MultipartForm.RemoveAll()
	if !s.checkCSRF(r) {
		s.fail(w, r, http.StatusForbidden, "", `{"error":"csrf"}`)
		return
	}
	if !s.checkNotBanned(w, r) {
		return
	}
	if !hasPoint(u, perm.UploadUse) {
		s.fail(w, r, http.StatusForbidden, "", `{"error":"没有上传权限"}`)
		return
	}
	// 外链模式：本站上传整体关闭
	if !sets.UploadEnabled {
		s.fail(w, r, http.StatusForbidden, "", `{"error":"本站已切换为仅外链模式，请使用外部图片/附件链接"}`)
		return
	}
	// 磁盘阈值守护（ROADMAP 5.5）：占用达上限即拒绝新上传
	if gb := sets.UploadMaxDiskGB; gb > 0 && s.uploadDirBytes() >= int64(gb)<<30 {
		s.fail(w, r, http.StatusInsufficientStorage, "", `{"error":"站点存储已达上限，暂时无法上传，请联系管理员"}`)
		return
	}
	kind := r.FormValue("kind")
	action := "upload.image"
	if kind == "file" {
		action = "upload.file"
	}
	decision := s.memberDecision(r, action, 0, nil, nil)
	if !decision.Allowed {
		if decision.Limit >= 0 && decision.Used >= decision.Limit {
			s.fail(w, r, 429, "MEMBER_QUOTA_EXCEEDED", decision.Reason)
		} else {
			s.fail(w, r, 403, "MEMBER_PERMISSION_DENIED", decision.Reason)
		}
		return
	}
	memberLimits := membershipOf(r).Member.Level.Limits
	if !s.allowKey("up:"+strconv.FormatInt(u.ID, 10), 30, time.Hour) {
		s.fail(w, r, http.StatusTooManyRequests, "", `{"error":"上传过于频繁"}`)
		return
	}
	limit, allowed, errMsg := int64(sets.MaxImageMB)<<20, allowedImageMime, "仅支持 JPG/PNG/GIF/WebP 图片"
	if kind == "file" {
		limit, allowed, errMsg = int64(sets.MaxFileMB)<<20, allowedFileMime, "仅支持 PDF/TXT/ZIP 附件"
	}
	memberLimit := memberLimits.ImageBytes
	if kind == "file" {
		memberLimit = memberLimits.FileBytes
	}
	if memberLimit >= 0 && memberLimit < limit {
		limit = memberLimit
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "", `{"error":"missing file"}`)
		return
	}
	defer f.Close()
	if hdr.Size > limit {
		s.fail(w, r, http.StatusRequestEntityTooLarge, "", fmt.Sprintf(`{"error":"文件超过 %dMB 限制"}`, limit>>20))
		return
	}

	// 嗅探真实类型，杜绝伪造扩展名
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	mime := http.DetectContentType(buf[:n])
	if i := strings.Index(mime, ";"); i > 0 { // 去掉 charset 等参数，统一与白名单键匹配
		mime = mime[:i]
	}
	if mime == "application/octet-stream" && kind == "file" {
		// zip 的部分变体可能被嗅探为 octet-stream，按原始扩展名兜底校验
		switch strings.ToLower(filepath.Ext(hdr.Filename)) {
		case ".zip", ".pdf", ".txt":
			mime = map[string]string{".zip": "application/zip", ".pdf": "application/pdf", ".txt": "text/plain"}[strings.ToLower(filepath.Ext(hdr.Filename))]
		}
	}
	ext, ok := allowed[mime]
	if !ok {
		s.fail(w, r, http.StatusUnsupportedMediaType, "", `{"error":"`+errMsg+`"}`)
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}

	countDay, err := s.st.ReserveMemberQuota(r.Context(), u.ID, "upload", 1, memberLimits.UploadsPerDay)
	if err != nil {
		s.memberError(w, r, err)
		return
	}
	bytesDay, err := s.st.ReserveMemberQuota(r.Context(), u.ID, "upload.bytes", hdr.Size, memberLimits.UploadBytesPerDay)
	quotaSaved := false
	defer func() {
		if !quotaSaved {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.st.RefundMemberQuota(ctx, u.ID, countDay, "upload", 1)
			_ = s.st.RefundMemberQuota(ctx, u.ID, bytesDay, "upload.bytes", hdr.Size)
		}
	}()
	if err != nil {
		s.memberError(w, r, err)
		return
	}
	now := time.Now()
	sub := filepath.Join(s.cfg.UploadDir, now.Format("2006/01"))
	if err := os.MkdirAll(sub, 0o755); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	var randBytes [8]byte
	_, _ = rand.Read(randBytes[:])
	diskPath := filepath.Join(sub, fmt.Sprintf("%s%s", hex.EncodeToString(randBytes[:]), ext))
	dst, err := os.Create(diskPath)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	saved := false
	defer func() {
		_ = dst.Close()
		if !saved {
			_ = os.Remove(diskPath)
		}
	}()
	written, err := io.Copy(dst, f)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	if err := dst.Close(); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}

	webPath := "/uploads/" + now.Format("2006/01") + "/" + filepath.Base(diskPath)
	if err := s.st.SaveUpload(r.Context(), u.ID, hdr.Filename, webPath, written, mime); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	saved = true
	quotaSaved = true
	s.respond(w, 201, map[string]any{"url": webPath, "name": hdr.Filename, "kind": kind, "mime": mime})
}

// GET /avatar/{uid}：自定义头像（资料页上传）优先，否则确定性字母头像 SVG。
func (s *Server) avatarSVG(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r, "uid")
	if p := s.customAvatarPath(uid); p != "" {
		f, err := os.Open(p)
		if err == nil {
			defer f.Close()
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.ServeContent(w, r, filepath.Base(p), time.Now(), f)
			return
		}
	}
	name, err := s.st.NameByID(r.Context(), uid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(avatar.SVG(uid, name)))
}

// GET /api/likes/{pid}：点赞名单。
func (s *Server) likesList(w http.ResponseWriter, r *http.Request) {
	pid := pathID(r, "pid")
	if p, _ := s.visiblePost(w, r, pid); p == nil {
		return
	}
	likers, err := s.st.Likers(r.Context(), pid)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", `{"error":"internal"}`)
		return
	}
	if likers == nil {
		likers = []store.Liker{}
	}
	s.respond(w, 200, mapRows(likers, func(v store.Liker) map[string]any { return map[string]any{"userId": idString(v.UID), "name": v.Name} }))
}

// GET /uploads/...：运行时上传文件服务（长缓存）。
// 只服务有上传记录的具体文件：禁止目录列表，无记录路径一律 404
// （杜绝逐级枚举目录与猜未挂载文件）；挂载到非公开楼层（待审/已删/
// 待审主题）的附件仅作者与管理人员可取，公开楼层附件对所有人开放。
func (s *Server) serveUploads(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if name == "" || strings.HasSuffix(name, "/") {
		http.NotFound(w, r)
		return
	}
	name = path.Clean(name)
	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		http.NotFound(w, r)
		return
	}
	// 头像本就是公开资料（/avatar/{uid} 直接展示），保持可访问
	if strings.HasPrefix(name, "avatars/") {
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.ServeFile(w, r, filepath.Join(s.cfg.UploadDir, filepath.FromSlash(name)))
		return
	}
	up, err := s.st.UploadByPath(r.Context(), "/uploads/"+name)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "", "internal")
		return
	}
	if up.PostID > 0 {
		var fid int64
		if err := s.st.UploadForum(r.Context(), up.PostID, &fid); err != nil || !canReadForum(r, fid) {
			http.NotFound(w, r)
			return
		}
		if !s.memberDecision(r, "attachment.download", fid, nil, nil).Allowed {
			s.fail(w, r, 403, "MEMBER_PERMISSION_DENIED", "当前等级无权下载附件")
			return
		}
	}
	public := up.PostID > 0 && !up.PostPending && !up.PostDeleted && !up.ThreadPending && !up.ThreadDeleted
	if !public {
		// 未挂载的新上传（编辑器预览期）与挂载在非公开楼层的附件：
		// 仅上传者本人与管理人员可取
		u := User(r)
		if u == nil || (u.ID != up.UID && !hasPoint(u, perm.AdminPanel) && !(up.PostID > 0 && s.canReadUpload(r, up.PostID))) {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(s.cfg.UploadDir, filepath.FromSlash(name)))
}

// uploadsPathRe 从 Markdown 内容中提取本站上传文件路径。
var uploadsPathRe = regexp.MustCompile(`/uploads/\d{4}/\d{2}/[0-9a-f]+\.[A-Za-z0-9]+`)

// linkUploads 发帖/编辑成功后把内容中引用的上传挂到楼层。
func (s *Server) linkUploads(r *http.Request, uid, postID int64, content string) {
	var paths []string
	for _, m := range uploadsPathRe.FindAllString(content, -1) {
		if len(paths) > 0 && paths[len(paths)-1] == m {
			continue
		}
		paths = append(paths, m)
	}
	_ = s.st.LinkUploadsToPost(r.Context(), uid, postID, paths)
}

// customAvatarPath 自定义头像文件（约定 data/uploads/avatars/uid.<ext>）。
func (s *Server) customAvatarPath(uid int64) string {
	matches, _ := filepath.Glob(filepath.Join(s.cfg.UploadDir, "avatars", strconv.FormatInt(uid, 10)+".*"))
	for _, m := range matches {
		if strings.HasSuffix(m, ".part") {
			continue
		}
		return m
	}
	return ""
}

// uploadDirBytes 上传目录磁盘占用（5 分钟缓存，避免每次上传全树遍历）。
func (s *Server) uploadDirBytes() int64 {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	if time.Now().Before(s.uploadSizeAt) {
		return s.uploadSize
	}
	var total int64
	_ = filepath.WalkDir(s.cfg.UploadDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	s.uploadSize = total
	s.uploadSizeAt = time.Now().Add(5 * time.Minute)
	return total
}

// ---- @提及通知 ----

var mentionRe = regexp.MustCompile(`@([\p{Han}a-zA-Z0-9_]{2,15})`)

// notifyMentions 解析内容中的 @用户名 并生成通知；返回被提及的人数。
// pending（待审核）内容不通知。
// notifyMentions 解析并投递 @提及 通知；返回被提及的用户 id 集合（供回复通知去重）。
func (s *Server) notifyMentions(r *http.Request, from *store.User, content string, th *store.Thread, p *store.Post) map[int64]bool {
	if p.Pending || th.Pending {
		return nil
	}
	names := mentionRe.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	var unique []string
	for _, m := range names {
		name := m[1]
		if name == from.Username || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
		if len(unique) >= 10 { // 单帖最多提醒 10 人
			break
		}
	}
	if len(unique) == 0 {
		return nil
	}
	users, err := s.st.UsersByNames(r.Context(), unique)
	if err != nil || len(users) == 0 {
		return nil
	}
	excerpt := truncate(strings.TrimSpace(content), 60)
	rows := make([]*store.Notification, 0, len(users))
	for _, target := range users {
		rows = append(rows, &store.Notification{
			UID:      target.ID,
			FromUID:  from.ID,
			FromName: from.Username,
			Type:     "mention",
			ThreadID: th.ID,
			PostID:   p.ID,
			Excerpt:  excerpt,
		})
	}
	s.deliverNotifications(r, from, users, rows, excerpt, func(target *store.User, link string) {
		if s.mailer.Enabled() {
			s.mailer.NotifyMention(target.Email, from.Username, th.Title, link, excerpt)
		}
	})
	out := map[int64]bool{}
	for _, target := range users {
		out[target.ID] = true
	}
	return out
}

// notifyReply 回复通知楼主（ROADMAP 运营 P0）：与 @ 提及去重，不通知自己。
func (s *Server) notifyReply(r *http.Request, from *store.User, th *store.Thread, p *store.Post, mentioned map[int64]bool) {
	if p.Pending || th.Pending || p.Floor == 1 {
		return
	}
	if mentioned == nil {
		mentioned = map[int64]bool{}
	}
	targetID, err := s.st.ReplyTargetAuthor(r.Context(), p.ID)
	if err != nil {
		return
	}
	for _, recipient := range []struct {
		uid  int64
		kind string
	}{{targetID, "reply.direct"}, {th.AuthorID, "reply"}} {
		if recipient.uid == 0 || recipient.uid == from.ID || mentioned[recipient.uid] {
			continue
		}
		mentioned[recipient.uid] = true
		author, err := s.st.UserByID(r.Context(), recipient.uid)
		if err != nil {
			continue
		}
		excerpt := truncate(strings.TrimSpace(p.ContentMD), 60)
		row := &store.Notification{
			UID:      author.ID,
			FromUID:  from.ID,
			FromName: from.Username,
			Type:     recipient.kind,
			ThreadID: th.ID,
			PostID:   p.ID,
			Excerpt:  excerpt,
		}
		s.deliverNotifications(r, from, []*store.User{author}, []*store.Notification{row}, excerpt,
			func(target *store.User, link string) {
				if s.mailer.Enabled() {
					s.mailer.NotifyReply(target.Email, from.Username, th.Title, link, excerpt)
				}
			})
	}
}

// deliverNotifications 落库 + 邮件 + SSE 实时提醒的共用投递通道。
func (s *Server) deliverNotifications(r *http.Request, from *store.User,
	targets []*store.User, rows []*store.Notification, excerpt string, mail func(*store.User, string)) {
	if len(rows) == 0 {
		return
	}
	filteredRows := []*store.Notification{}
	filteredTargets := []*store.User{}
	for i, target := range targets {
		if target.IsBlocked() || i >= len(rows) {
			continue
		}
		rr, err := s.loadMembership(requestWithUser(r, target))
		if err != nil {
			continue
		}
		th, err := s.st.Thread(rr.Context(), rows[i].ThreadID)
		if err != nil || th.Pending || !s.canViewThread(rr, th) {
			continue
		}
		rows[i].EventKey = "post:" + strconv.FormatInt(rows[i].PostID, 10)
		filteredRows = append(filteredRows, rows[i])
		filteredTargets = append(filteredTargets, target)
	}
	rows, targets = filteredRows, filteredTargets
	if len(rows) == 0 {
		return
	}
	link := func(target *store.User) string {
		return s.cfg.SiteURL + ThreadURL(rows[0].ThreadID, 1) + "#post" + strconv.FormatInt(rows[0].PostID, 10)
	}
	if err := s.st.AddNotifications(r.Context(), rows); err != nil {
		return
	}
	for i, target := range targets {
		if rows[i].ID == 0 {
			continue
		}
		prefs, err := s.st.NotificationPreferences(r.Context(), target.ID)
		if err == nil && prefs["email"] {
			mail(target, link(target))
		}
		rr, err := s.loadMembership(requestWithUser(r, target))
		if err != nil {
			continue
		}
		count := s.st.UnreadCount(rr.Context(), target.ID)
		s.publish("u:"+strconv.FormatInt(target.ID, 10), eventBody{
			Type: "notify", FromName: from.Username, NotifyCount: int(count),
		})
	}
}

// ---- 通知页 ----

func (s *Server) canReadUpload(r *http.Request, pid int64) bool {
	p, err := s.st.Post(r.Context(), pid)
	if err != nil {
		return false
	}
	th, err := s.st.Thread(r.Context(), p.ThreadID)
	return err == nil && s.canModerateThread(r, th)
}
