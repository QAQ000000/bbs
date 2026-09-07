// SPDX-License-Identifier: AGPL-3.0-or-later
// store/settings.go：站点设置 KV 的类型化读取。
package store

import (
	"context"
	"strconv"
)

// SiteSettings 站点可动态修改的设置（settings KV 表的类型化视图）。
type SiteSettings struct {
	Version          int64  `json:"version"`
	SiteName         string `json:"siteName"`
	ThreadsPerPage   int    `json:"threadsPerPage"`
	PostsPerPage     int    `json:"postsPerPage"`
	RegisterEnabled  bool   `json:"registerEnabled"`
	SiteClosed       bool   `json:"siteClosed"`
	SiteClosedReason string `json:"siteClosedReason"`
	ModerateEnabled  bool   `json:"moderateEnabled"`

	UploadEnabled   bool `json:"uploadEnabled"`
	MaxImageMB      int  `json:"maxImageMB"`
	MaxFileMB       int  `json:"maxFileMB"`
	UploadMaxDiskGB int  `json:"uploadMaxDiskGB"`

	CaptchaEnabled     bool   `json:"captchaEnabled"`
	EmailVerifyEnabled bool   `json:"emailVerifyEnabled"`
	RequireConsent     bool   `json:"requireConsent"`
	TermsContent       string `json:"termsContent"`
	PrivacyContent     string `json:"privacyContent"`
	SiteLogo           string `json:"siteLogo"`
	FooterText         string `json:"footerText"`
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

1. **收集的信息**：注册时收集的用户名、（选填）邮箱；您主动填写的签名与发布的内容；登录安全审计与后台操作中记录的 IP 地址与时间（用于反垃圾与治理）。
2. **信息用途**：用于提供论坛服务、账号找回、@提及提醒、安全审计与治理，不用于对外出售。
3. **邮箱验证**：开启邮箱验证的站点会将您的邮箱用于发送验证邮件与服务通知，邮件由本站直接发送。
4. **信息存储**：数据存储于本站数据库与上传目录；密码以不可逆的哈希形式保存，找回密码令牌仅存哈希。
5. **您的权利**：您可在「资料设置」中查看与修改个人资料、导出本人发布的全部内容（JSON 格式）；如需删除账号，请联系站长处理。
6. **政策更新**：本政策如有重大变更将在本页公告。

如有隐私相关疑问或请求，请联系站长。`

func defaultSettings() SiteSettings {
	return SiteSettings{
		Version:          1,
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

// EnsureSettingsDefaults 写入缺失的设置项（已存在的键不覆盖）。
func (s *Store) EnsureSettingsDefaults(ctx context.Context, kv map[string]string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	version, err := lockSettings(ctx, tx)
	if err != nil {
		return err
	}
	inserted := int64(0)
	for k, v := range kv {
		known := false
		for _, f := range SiteSettingFields() {
			if f.LegacyName == k {
				known = true
				v, _, err = normalizeSetting(f, v)
				if err != nil {
					return err
				}
				break
			}
		}
		if !known {
			return &SettingsValidationError{k, "未知设置"}
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO settings (key, value) VALUES ($1,$2) ON CONFLICT (key) DO NOTHING`, k, v)
		if err != nil {
			return err
		}
		inserted += tag.RowsAffected()
	}
	if inserted > 0 {
		if _, err = tx.Exec(ctx, `UPDATE settings SET value=$2 WHERE key=$1`, settingsVersionKey, strconv.FormatInt(version+1, 10)); err != nil {
			return err
		}
	}
	if _, err = readSettings(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
