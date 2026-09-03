// SPDX-License-Identifier: AGPL-3.0-or-later
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
	SiteName        string
	ThreadsPerPage  int
	PostsPerPage    int
	RegisterEnabled bool
	SiteClosed      bool
	SiteClosedReason string
	ModerateEnabled bool // 新用户发帖需审核

	UploadEnabled bool // 本站上传开关（关闭 = 仅外链模式）
	MaxImageMB    int  // 图片上传上限（MB）
	MaxFileMB     int  // 附件上传上限（MB）
}

func defaultSettings() SiteSettings {
	return SiteSettings{
		SiteName:        "GoBBS 社区",
		ThreadsPerPage:  20,
		PostsPerPage:    10,
		RegisterEnabled: true,
		SiteClosed:      false,
		SiteClosedReason: "站点维护中，请稍后再访。",
		UploadEnabled:   true,
		MaxImageMB:      8,
		MaxFileMB:       20,
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
}
