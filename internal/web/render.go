// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"dzforum/assets"
	"dzforum/internal/avatar"
	"dzforum/internal/markdown"
	"dzforum/internal/perm"
	"dzforum/internal/smiley"
	"dzforum/internal/store"
)

func toHTML(s string) template.HTML { return template.HTML(s) }

// canEditContent 编辑权限：ContentEditAny（管理员）或 ContentEditOwn + 本人。
func canEditContent(viewer *store.User, authorID int64) bool {
	if viewer == nil {
		return false
	}
	role := perm.RoleFromGroupID(viewer.GroupID)
	if perm.Allowed(role, perm.ContentEditAny) {
		return true
	}
	return perm.Allowed(role, perm.ContentEditOwn) && viewer.ID == authorID
}

// canDeleteContent 删除权限：ContentDeleteAny 或 ContentDeleteOwn + 本人。
func canDeleteContent(viewer *store.User, authorID int64) bool {
	if viewer == nil {
		return false
	}
	role := perm.RoleFromGroupID(viewer.GroupID)
	if perm.Allowed(role, perm.ContentDeleteAny) {
		return true
	}
	return perm.Allowed(role, perm.ContentDeleteOwn) && viewer.ID == authorID
}

// assetQuery 静态资源内容指纹：模板引用 /static/...?v=<指纹>，
// 内容一变 URL 即变，长缓存与即时失效兼得。
var assetQuery = func() string {
	h := sha256.New()
	sub := assets.Static()
	_ = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if b, err := fs.ReadFile(sub, path); err == nil {
			h.Write([]byte(path))
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

// ---- 分页 ----

// PageItem 分页条条目；IsEllipsis 为真时仅展示省略号。
type PageItem struct {
	N          int
	Label      string
	URL        string
	Cur        bool
	IsEllipsis bool
}

// BuildPage 生成 经典论坛风格分页条：1 2 3 … 8 下一页。
func BuildPage(cur, total int, urlFor func(int) string) []PageItem {
	if total <= 1 {
		return nil
	}
	var items []PageItem
	add := func(n int, label string) {
		items = append(items, PageItem{N: n, Label: label, URL: urlFor(n), Cur: n == cur})
	}
	start := cur - 2
	if start < 1 {
		start = 1
	}
	end := start + 5
	if end > total {
		end = total
	}
	if start > 1 {
		add(1, "1")
		if start > 2 {
			items = append(items, PageItem{Label: "…", IsEllipsis: true})
		}
	}
	for i := start; i <= end; i++ {
		add(i, strconv.Itoa(i))
	}
	if end < total {
		if end < total-1 {
			items = append(items, PageItem{Label: "…", IsEllipsis: true})
		}
		add(total, strconv.Itoa(total))
	}
	if cur < total {
		add(cur+1, "下一页 ›")
	}
	return items
}

// ---- 视图模型 ----

type Common struct {
	SiteName    string
	SiteLogo    string
	Title       string
	User        *store.User
	CSRF        string
	Flash       string
	NavActive   string
	Year        int
	NotifyCount int64  // 未读通知数（登录用户）
	AssetQuery  string // 静态资源内容指纹版本参数
	MetaDesc    string // SEO：页面摘要（帖子页/版块页填充）
}

type PostVM struct {
	store.Post
	HTML      template.HTML
	Avatar    template.HTML
	Editable  bool
	Deletable bool   // 与编辑分离：版主可删管辖版块内容但不可编辑他人内容
	CanLike   bool   // 可点赞（登录且非本人楼层）
	Quotable  bool   // 可引用（登录即可）
	CanReport bool   // 可举报（登录且非本人楼层）
	CSRF      string // 删除表单用
}

// PostVMOf 构建楼层视图模型；viewer 为 nil 时（SSE 广播）无编辑权限。
// Deletable 初值按全局权限点计算，版主管辖范围由页面层（handleThread）收窄。
func PostVMOf(p *store.Post, viewer *store.User, csrf string) *PostVM {
	return &PostVM{
		Post:      *p,
		HTML:      toHTML(p.ContentHTML),
		Avatar:    toHTML(avatar.HTML(p.AuthorID, p.AuthorName)),
		Editable:  canEditContent(viewer, p.AuthorID),
		Deletable: canDeleteContent(viewer, p.AuthorID),
		CanLike:   viewer != nil && viewer.ID != p.AuthorID,
		Quotable:  viewer != nil,
		CanReport: viewer != nil && viewer.ID != p.AuthorID,
		CSRF:      csrf,
	}
}

// SmileyGroup 编辑器表情面板的一组。
type SmileyGroup = smiley.Group

// SmileyGroups 返回表情分组（内置 emoji + 站点导入的图片包）。
func SmileyGroups() []SmileyGroup {
	return smiley.Groups()
}

// ---- 模板函数 ----

func funcMap() template.FuncMap {
	return template.FuncMap{
		"avatar": func(uid int64, name string) template.HTML {
			return template.HTML(avatar.HTML(uid, name))
		},
		"avatarS": func(uid int64, name string) template.HTML {
			return template.HTML(avatar.Small(uid, name))
		},
		"timefmt":  timefmt,
		"markdown": func(s string) template.HTML { return toHTML(markdown.Render(s)) },
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"forumURL": func(fid any) string {
			switch v := fid.(type) {
			case int64:
				return ForumURL(v, 1)
			case int:
				return ForumURL(int64(v), 1)
			}
			return "#"
		},
		"threadURL": func(tid any) string {
			switch v := tid.(type) {
			case int64:
				return ThreadURL(v, 1)
			case int:
				return ThreadURL(int64(v), 1)
			}
			return "#"
		},
		"userURL": UserURL,
		"postVM":  PostVMOf,
		"threadNext": func(tid int64) string {
			return urlQueryEscape(ThreadURL(tid, 1))
		},
		"indexFirst": func(s string) string {
			for _, r := range strings.TrimSpace(s) {
				return strings.ToUpper(string(r))
			}
			return "版"
		},
		"smileyURL":   func(c smiley.Code) string { return smiley.URL(c) },
		"smileyIsImg": func(c smiley.Code) bool { return c.File != "" },
		"reasonLabel": func(code string) string {
			if l, ok := ReasonLabels[code]; ok {
				return l
			}
			return code
		},
		"inc": func(n int) int { return n + 1 },
		"dec": func(n int) int { return n - 1 },
	}
}

// timefmt 相对时间显示，参考经典论坛的显示习惯。
func timefmt(t time.Time) string {
	if t.IsZero() || t.Equal(time.Unix(0, 0)) {
		return ""
	}
	now := time.Now()
	if t.After(now) { // 未来时间（如禁言截止）：显示绝对日期避免误导
		return t.Format("2006-1-2 15:04")
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case t.After(today):
		return "今天 " + t.Format("15:04")
	case t.After(today.AddDate(0, 0, -1)):
		return "昨天 " + t.Format("15:04")
	case t.After(today.AddDate(0, 0, -7)):
		return fmt.Sprintf("%d 天前", int(today.Sub(t).Hours()/24)+1)
	case t.Year() != now.Year():
		return t.Format("2006-1-2 15:04")
	default:
		return t.Format("1-2 15:04")
	}
}

// ---- 渲染器 ----

type pageSet struct {
	t      *template.Template
	layout string // 该集合的布局模板名
}

// Renderer 按页面名缓存模板集；DevMode 下每次重载。
type Renderer struct {
	dev    bool
	pages  map[string]pageSet
	live   *template.Template
	baseFS fs.FS
}

func NewRenderer(dev bool) (*Renderer, error) {
	r := &Renderer{dev: dev, baseFS: assets.Templates(), pages: map[string]pageSet{}}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Renderer) load() error {
	fsys := r.baseFS
	base := template.New("layout.html").Funcs(funcMap())
	base, err := base.ParseFS(fsys, "layout.html", "p_*.html")
	if err != nil {
		return fmt.Errorf("解析基础模板: %w", err)
	}
	pages := map[string]pageSet{}
	pageFiles, err := fs.Glob(fsys, "page_*.html")
	if err != nil {
		return err
	}
	for _, pf := range pageFiles {
		t := template.Must(base.Clone())
		if _, err := t.ParseFS(fsys, pf); err != nil {
			return fmt.Errorf("解析页面模板 %s: %w", pf, err)
		}
		pages[pf] = pageSet{t: t, layout: "layout.html"}
	}
	// 后台模板集：独立的 admin_layout.html 布局
	adminBase, err := template.New("admin_layout.html").Funcs(funcMap()).
		ParseFS(fsys, "admin_layout.html", "p_pagination.html")
	if err != nil {
		return fmt.Errorf("解析后台基础模板: %w", err)
	}
	adminFiles, err := fs.Glob(fsys, "admin_*.html")
	if err != nil {
		return err
	}
	for _, af := range adminFiles {
		if af == "admin_layout.html" {
			continue
		}
		t := template.Must(adminBase.Clone())
		if _, err := t.ParseFS(fsys, af); err != nil {
			return fmt.Errorf("解析后台页面模板 %s: %w", af, err)
		}
		pages[af] = pageSet{t: t, layout: "admin_layout.html"}
	}

	liveSet := template.Must(template.New("live").Funcs(funcMap()).ParseFS(fsys, "p_*.html"))
	r.pages, r.live = pages, liveSet
	return nil
}

// Render 执行页面模板。
func (r *Renderer) Render(w io.Writer, page string, data any) error {
	if r.dev {
		if err := r.load(); err != nil {
			return err
		}
	}
	ps, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("未知页面模板 %s", page)
	}
	err := ps.t.ExecuteTemplate(w, ps.layout, data)
	if err != nil {
		slog.Error("模板渲染失败", "page", page, "err", err)
	}
	return err
}

// RenderPartial 渲染片段模板（SSE 推送的 HTML 用）。
func (r *Renderer) RenderPartial(name string, data any) (string, error) {
	var buf iofsBuffer
	if err := r.live.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return string(buf), nil
}

type iofsBuffer []byte

func (b *iofsBuffer) Write(p []byte) (int, error) { *b = append(*b, p...); return len(p), nil }
func (b iofsBuffer) String() string               { return string(b) }
