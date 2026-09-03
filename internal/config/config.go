// SPDX-License-Identifier: AGPL-3.0-or-later
// Package config 提供服务配置：环境变量优先，均带合理默认值。
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 是论坛服务的全部运行配置。
type Config struct {
	Addr string // HTTP 监听地址
	DSN  string // PostgreSQL 连接串

	SiteName  string
	SiteLogo  string
	DevMode   bool          // 开发模式：每次请求重载模板
	ProdMode  bool          // 生产模式：加安全头、关闭调试输出
	CookieTTL time.Duration // 会话 Cookie 有效期（同时是服务端会话时长）

	ThreadsPerPage int // 版块页每页主题数
	PostsPerPage   int // 帖子页每页楼层数

	UploadDir string // 图片上传存储目录（运行时数据，不打包进二进制）
	SmileyDir string // 自定义图片表情包目录（运行时数据）

	SiteURL      string // 站点外部可达地址（邮件里的链接）
	SMTPHost     string // 为空则禁用邮件通知
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string // 发件人地址
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getint(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// FromEnv 从环境变量构建配置。
func FromEnv() Config {
	return Config{
		Addr: getenv("FORUM_ADDR", "127.0.0.1:8080"),
		DSN:  getenv("FORUM_DSN", "postgres://123456:123456@127.0.0.1:5432/forum"),

		SiteName: getenv("FORUM_SITE_NAME", "GoBBS 社区"),
		SiteLogo: getenv("FORUM_SITE_LOGO", "Go!BBS"),
		DevMode:  getenv("FORUM_DEV", "") == "1",
		ProdMode: getenv("FORUM_PROD", "") == "1",

		UploadDir:      getenv("FORUM_UPLOAD_DIR", "data/uploads"),
		SmileyDir:      getenv("FORUM_SMILEY_DIR", "data/smiley"),

		SiteURL:      getenv("FORUM_SITE_URL", "http://127.0.0.1:8090"),
		SMTPHost:     getenv("FORUM_SMTP_HOST", ""),
		SMTPPort:     getint("FORUM_SMTP_PORT", 25),
		SMTPUser:     getenv("FORUM_SMTP_USER", ""),
		SMTPPassword: getenv("FORUM_SMTP_PASS", ""),
		SMTPFrom:     getenv("FORUM_SMTP_FROM", "noreply@gobbs.local"),

		CookieTTL:      30 * 24 * time.Hour,
		ThreadsPerPage: getint("FORUM_THREADS_PER_PAGE", 20),
		PostsPerPage:   getint("FORUM_POSTS_PER_PAGE", 10),
	}
}
