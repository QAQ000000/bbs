// SPDX-License-Identifier: AGPL-3.0-or-later
// Package store 数据访问层：所有 SQL 集中在此，web 层只见领域对象。
package store

import (
	"time"
)

// User 论坛用户。group_id：0=会员 1=管理员 2=版主。
type User struct {
	ID                 int64     `db:"id"`
	Username           string    `db:"username"`
	PasswordHash       string    `db:"password_hash"`
	Email              string    `db:"email"`
	GroupID            int       `db:"group_id"`
	PostCount          int64     `db:"post_count"`
	Signature          string    `db:"signature"`
	CreatedAt          time.Time `db:"created_at"`
	PostsRead          int64     `db:"posts_read"`
	DaysVisited        int       `db:"days_visited"`
	EmailVerified      bool      `db:"email_verified"`
	MustChangePassword bool      `db:"must_change_password"` // -seed 初始账号首次登录强制改密
	BlockedUntil       time.Time `db:"blocked_until"`        // 封禁（禁止登录）截止；epoch=未封禁
}

// IsBlocked 账号是否处于封禁期（禁止登录；infinity=永久）。
func (u *User) IsBlocked() bool { return u.BlockedUntil.After(time.Now()) }

func (u *User) IsAdmin() bool { return u.GroupID == 1 }

// IsStaff 管理员或版主。
func (u *User) IsStaff() bool { return u.GroupID == 1 || u.GroupID == 2 }

// IsModerator 版主。
func (u *User) IsModerator() bool { return u.GroupID == 2 }

// Category 板块分组。
type Category struct {
	ID     int
	Name   string
	Forums []*Forum
}

// Forum 版块（含为列表页反范式化的统计字段）。
type Forum struct {
	ID          int64
	CategoryID  int
	Name        string
	Description string

	ThreadCount int64
	PostCount   int64
	TodayCount  int

	LastPostAt      time.Time
	HasLastPost     bool
	LastPostUID     int64
	LastPostAuthor  string
	LastThreadID    int64
	LastThreadTitle string
	Moderators      string // 后台表单展示用 CSV；权威数据在 forum_moderators
}

// Thread 主题。
type Thread struct {
	ID            int64
	ForumID       int64
	AuthorID      int64
	AuthorName    string
	Title         string
	Sticky        int
	Digest        bool
	Closed        bool
	PostCount     int
	ViewCount     int64
	CreatedAt     time.Time
	LastPostAt    time.Time
	LastPostUID   int64
	LastPostName  string
	FirstPostID   int64
	Pending       bool   // 待审核（阶段三）
	PendingReason string // 进入审核的原因
}

// Replies 返回回复数（不含首楼）。
func (t *Thread) Replies() int {
	if t.PostCount > 0 {
		return t.PostCount - 1
	}
	return 0
}

// Post 楼层。
type Post struct {
	ID            int64
	ThreadID      int64
	AuthorID      int64
	AuthorName    string
	AuthorGroup   int
	Floor         int
	ContentMD     string
	ContentHTML   string
	CreatedAt     time.Time
	EditedAt      time.Time
	HasEdited     bool
	Pending       bool   // 待审核（阶段三）
	PendingReason string // 进入审核的原因（manual / newuser_link）
	LikeCount     int    // 点赞数（冗余回写）
	Version       int    // 编辑版本号（冲突检测）
	IP            string // 发布来源 IP（隐私政策声明，仅管理员可见掩码）
}

// Session 服务端会话。
type Session struct {
	ID        int64
	Token     string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
}

// SiteStats 首页统计条。
type SiteStats struct {
	TodayPosts   int64
	Yesterday    int64
	TotalPosts   int64
	TotalThreads int64
	Members      int64
}
