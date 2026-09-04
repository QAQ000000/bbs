// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, `{"error":"csrf"}`, http.StatusForbidden)
		return
	}
	if !s.checkNotBanned(w, r) {
		http.Error(w, `{"error":"banned"}`, http.StatusForbidden)
		return
	}
	if !hasPoint(u, perm.UploadUse) {
		http.Error(w, `{"error":"没有上传权限"}`, http.StatusForbidden)
		return
	}
	// 外链模式：本站上传整体关闭
	sets := s.sets(r)
	if !sets.UploadEnabled {
		http.Error(w, `{"error":"本站已切换为仅外链模式，请使用外部图片/附件链接"}`, http.StatusForbidden)
		return
	}
	kind := r.FormValue("kind")
	if !s.allowKey("up:"+strconv.FormatInt(u.ID, 10), 30, time.Hour) {
		http.Error(w, `{"error":"上传过于频繁"}`, http.StatusTooManyRequests)
		return
	}
	limit, allowed, errMsg := int64(sets.MaxImageMB)<<20, allowedImageMime, "仅支持 JPG/PNG/GIF/WebP 图片"
	if kind == "file" {
		limit, allowed, errMsg = int64(sets.MaxFileMB)<<20, allowedFileMime, "仅支持 PDF/TXT/ZIP 附件"
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	if err := r.ParseMultipartForm(limit); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"文件超过 %dMB 限制"}`, limit>>20), http.StatusRequestEntityTooLarge)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"missing file"}`, http.StatusBadRequest)
		return
	}
	defer f.Close()
	if hdr.Size > limit {
		http.Error(w, fmt.Sprintf(`{"error":"文件超过 %dMB 限制"}`, limit>>20), http.StatusRequestEntityTooLarge)
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
		http.Error(w, `{"error":"`+errMsg+`"}`, http.StatusUnsupportedMediaType)
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	now := time.Now()
	sub := filepath.Join(s.cfg.UploadDir, now.Format("2006/01"))
	if err := os.MkdirAll(sub, 0o755); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	var randBytes [8]byte
	_, _ = rand.Read(randBytes[:])
	diskPath := filepath.Join(sub, fmt.Sprintf("%s%s", hex.EncodeToString(randBytes[:]), ext))
	dst, err := os.Create(diskPath)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	written, err := io.Copy(dst, f)
	if err != nil {
		os.Remove(diskPath)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	webPath := "/uploads/" + now.Format("2006/01") + "/" + filepath.Base(diskPath)
	if err := s.st.SaveUpload(r.Context(), u.ID, hdr.Filename, webPath, written, mime); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(map[string]string{"url": webPath, "name": hdr.Filename, "kind": kind, "mime": mime})
	_, _ = w.Write(b)
}

// GET /avatar/{uid}：确定性字母头像 SVG（点赞浮层用）。
func (s *Server) avatarSVG(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r, "uid")
	name, err := s.st.NameByID(r.Context(), uid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(avatar.HTML(uid, name)))
}

// GET /api/likes/{pid}：点赞名单。
func (s *Server) likesList(w http.ResponseWriter, r *http.Request) {
	pid := pathID(r, "pid")
	likers, err := s.st.Likers(r.Context(), pid)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if likers == nil {
		likers = []store.Liker{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(likers)
	_, _ = w.Write(b)
}

// GET /uploads/...：运行时上传文件服务（长缓存）。
func (s *Server) serveUploads(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.cfg.UploadDir))).ServeHTTP(w, r)
}

// ---- @提及通知 ----

var mentionRe = regexp.MustCompile(`@([\p{Han}a-zA-Z0-9_]{2,15})`)

// notifyMentions 解析内容中的 @用户名 并生成通知；返回被提及的人数。
// pending（待审核）内容不通知。
func (s *Server) notifyMentions(r *http.Request, from *store.User, content string, th *store.Thread, p *store.Post) int {
	if p.Pending {
		return 0
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
		return 0
	}
	users, err := s.st.UsersByNames(r.Context(), unique)
	if err != nil || len(users) == 0 {
		return 0
	}
	excerpt := truncate(strings.TrimSpace(content), 60)
	link := s.cfg.SiteURL + ThreadURL(th.ID, 1) + "#post" + strconv.FormatInt(p.ID, 10)
	rows := make([]*store.Notification, 0, len(users))
	for _, target := range users {
		if s.mailer.Enabled() {
			s.mailer.NotifyMention(target.Email, from.Username, th.Title, link, excerpt)
		}
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
	if err := s.st.AddNotifications(r.Context(), rows); err != nil {
		return 0
	}
	// 每个被提及者一条实时提醒
	for _, target := range users {
		count := s.st.UnreadCount(r.Context(), target.ID)
		s.publish("u:"+strconv.FormatInt(target.ID, 10), eventBody{
			Type: "notify", FromName: from.Username, NotifyCount: int(count),
		})
	}
	return len(rows)
}

// ---- 通知页 ----

func (s *Server) notifyPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	u := User(r)
	list, err := s.st.Notifications(r.Context(), u.ID, 30)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "加载失败", err.Error())
		return
	}
	if list == nil {
		list = []*store.Notification{}
	}
	s.st.MarkNotificationsRead(r.Context(), u.ID)
	data := struct {
		Common
		Notifications []*store.Notification
	}{s.common(r), list}
	data.Title = "通知"
	_ = s.rd.Render(w, "page_notify.html", &data)
}
