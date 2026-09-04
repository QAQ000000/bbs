// SPDX-License-Identifier: AGPL-3.0-or-later
// store/settings.go：站点设置 KV 的类型化读取与 30s 进程内缓存。
package store

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SiteSettings 站点可动态修改的设置（settings KV 表的类型化视图）。
type SiteSettings struct {
	SiteName         string
	ThreadsPerPage   int
	PostsPerPage     int
	RegisterEnabled  bool
	SiteClosed       bool
	SiteClosedReason string
	ModerateEnabled  bool // 新用户发帖需审核

	UploadEnabled   bool // 本站上传开关（关闭 = 仅外链模式）
	MaxImageMB      int  // 图片上传上限（MB）
	MaxFileMB       int  // 附件上传上限（MB）
	UploadMaxDiskGB int  // 上传目录磁盘占用上限（GB），超过即拒绝新上传

	CaptchaEnabled     bool   // 注册算术验证码
	EmailVerifyEnabled bool   // 邮箱验证开关（SMTP 未启用时自动失效）
	RequireConsent     bool   // 注册需勾选同意条款与隐私政策
	TermsContent       string // 服务条款（Markdown，后台可编辑）
	PrivacyContent     string // 隐私政策（Markdown，后台可编辑）
}

// defaultTerms/defaultPrivacy 内置默认文案：开箱即有合规页面，站长可在后台改写。
const defaultTerms = `## 服务条款

欢迎使用本站。注册或使用本站服务即表示您同意以下条款：

1. **账号责任**：您对账号与密码安全负责，请勿出借、转让账号；因保管不善造成的损失由您自行承担。
2. **内容规范**：您发布的内容不得违反法律法规，不得侵犯他人合法权益（包括著作权、隐私权、名誉权），不得发布垃圾广告或恶意程序。
3. **内容授权**：您在本站发布的内容，授予本站为运营与展示目的所需的存储、展示与分发权利；内容的著作权仍归您所有。
4. **内容处置**：站长与版主有权对违规内容进行编辑、审核、隐藏或删除，对违规账号予以禁言或封禁。
5. **服务变更**：本站有权随运营需要调整功能或中止服务，并将尽合理努力提前公告。
6. **免责声明**：本站对第三方链接内容、用户言论及不可抗力导致的服务中断不承担责任。

如有疑问，请通过站内联系方式与站长取得联系。`

const defaultPrivacy = `## 隐私政策

本站尊重并保护用户隐私，说明如下：

1. **收集的信息**：注册时收集的用户名、（选填）邮箱；您主动填写的签名与发布的内容；登录与发布行为记录的 IP 地址与时间（用于安全审计与反垃圾）。
2. **信息用途**：用于提供论坛服务、账号找回、@提及提醒、安全审计与治理，不用于对外出售。
3. **邮箱验证**：开启邮箱验证的站点会将您的邮箱用于发送验证邮件与服务通知，邮件由本站直接发送。
4. **信息存储**：数据存储于本站数据库与上传目录；密码以不可逆的哈希形式保存，找回密码令牌仅存哈希。
5. **您的权利**：您可在「资料设置」中查看与修改个人资料、导出本人发布的全部内容（JSON 格式）；如需删除账号，请联系站长处理。
6. **政策更新**：本政策如有重大变更将在本页公告。

如有隐私相关疑问或请求，请联系站长。`

func defaultSettings() SiteSettings {
	return SiteSettings{
		SiteName:         "GoBBS 社区",
		ThreadsPerPage:   20,
		PostsPerPage:     10,
		RegisterEnabled:  true,
		SiteClosed:       false,
		SiteClosedReason: "站点维护中，请稍后再访。",
		UploadEnabled:    true,
		MaxImageMB:       8,
		MaxFileMB:        20,
		UploadMaxDiskGB:  10,
		RequireConsent:   true,
		TermsContent:     defaultTerms,
		PrivacyContent:   defaultPrivacy,
	}
}

var (
	settingsMu     sync.RWMutex
	settingsCache  *SiteSettings
	settingsExpiry time.Time
)

// Settings 读取站点设置（30s 进程内缓存）。
func (s *Store) Settings(ctx context.Context) SiteSettings {
	settingsMu.RLock()
	if settingsCache != nil && time.Now().Before(settingsExpiry) {
		v := *settingsCache
		settingsMu.RUnlock()
		return v
	}
	settingsMu.RUnlock()

	st := defaultSettings()
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM settings`)
	if err == nil {
		defer rows.Close()
		kv := map[string]string{}
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				kv[k] = v
			}
		}
		applySettings(&st, kv)
	}

	settingsMu.Lock()
	settingsCache = &st
	settingsExpiry = time.Now().Add(30 * time.Second)
	settingsMu.Unlock()
	return st
}

// SaveSettings 覆盖写入设置并立即使缓存失效。
func (s *Store) SaveSettings(ctx context.Context, kv map[string]string) error {
	for k, v := range kv {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO settings (key, value) VALUES ($1,$2)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, k, v); err != nil {
			return err
		}
	}
	settingsMu.Lock()
	settingsCache = nil
	settingsMu.Unlock()
	return nil
}

// EnsureSettingsDefaults 写入缺失的设置项（已存在的键不覆盖）。
func (s *Store) EnsureSettingsDefaults(ctx context.Context, kv map[string]string) error {
	for k, v := range kv {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO settings (key, value) VALUES ($1,$2) ON CONFLICT (key) DO NOTHING`, k, v); err != nil {
			return err
		}
	}
	return nil
}

func applySettings(st *SiteSettings, kv map[string]string) {
	if v, ok := kv["site_name"]; ok && strings.TrimSpace(v) != "" {
		st.SiteName = v
	}
	if v, ok := kv["threads_per_page"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 && n <= 100 {
			st.ThreadsPerPage = n
		}
	}
	if v, ok := kv["posts_per_page"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 && n <= 100 {
			st.PostsPerPage = n
		}
	}
	if v, ok := kv["register_enabled"]; ok {
		st.RegisterEnabled = v != "0"
	}
	if v, ok := kv["site_closed"]; ok {
		st.SiteClosed = v == "1"
	}
	if v, ok := kv["site_closed_reason"]; ok && strings.TrimSpace(v) != "" {
		st.SiteClosedReason = v
	}
	if v, ok := kv["moderate_enabled"]; ok {
		st.ModerateEnabled = v == "1"
	}
	if v, ok := kv["upload_enabled"]; ok {
		st.UploadEnabled = v != "0"
	}
	if v, ok := kv["max_image_mb"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1024 {
			st.MaxImageMB = n
		}
	}
	if v, ok := kv["max_file_mb"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1024 {
			st.MaxFileMB = n
		}
	}
	if v, ok := kv["captcha_enabled"]; ok {
		st.CaptchaEnabled = v == "1"
	}
	if v, ok := kv["email_verify_enabled"]; ok {
		st.EmailVerifyEnabled = v == "1"
	}
	if v, ok := kv["upload_max_disk_gb"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1024 {
			st.UploadMaxDiskGB = n
		}
	}
	if v, ok := kv["require_consent"]; ok {
		st.RequireConsent = v == "1"
	}
	if v, ok := kv["terms_content"]; ok {
		st.TermsContent = v
	}
	if v, ok := kv["privacy_content"]; ok {
		st.PrivacyContent = v
	}
}
