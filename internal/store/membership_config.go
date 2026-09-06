// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Membership is independent of staff roles. No management permission is accepted here.
var MemberActions = []string{"forum.read", "thread.create", "post.reply", "post.edit", "post.delete", "post.like", "thread.favorite", "post.report", "upload.image", "upload.file", "attachment.download", "post.link.direct", "post.skip.moderate"}

type LevelBadge struct {
	Label      string `json:"label"`
	Icon       string `json:"icon"` // closed icon catalog, rendered by the frontend
	Color      string `json:"color"`
	Background string `json:"background"`
}
type MemberLimits struct {
	ThreadsPerDay      int64 `json:"threadsPerDay"`
	RepliesPerDay      int64 `json:"repliesPerDay"`
	UploadsPerDay      int64 `json:"uploadsPerDay"`
	UploadBytesPerDay  int64 `json:"uploadBytesPerDay"`
	ImageBytes         int64 `json:"imageBytes"`
	FileBytes          int64 `json:"fileBytes"`
	AttachmentsPerPost int64 `json:"attachmentsPerPost"`
	EditMinutes        int64 `json:"editMinutes"`
	SignatureLength    int64 `json:"signatureLength"`
}
type MemberLevel struct {
	ID            int             `json:"id"`
	Name          string          `json:"name"`
	Rank          int             `json:"rank"`
	Automatic     bool            `json:"automatic"`
	Experience    int64           `json:"experience"`
	DaysVisited   int             `json:"daysVisited"`
	PostsRead     int64           `json:"postsRead"`
	PostCount     int64           `json:"postCount"`
	EmailVerified bool            `json:"emailVerified"`
	Permissions   map[string]bool `json:"permissions"`
	Limits        MemberLimits    `json:"limits"`
	Badge         LevelBadge      `json:"badge"`
}
type GrowthRule struct {
	Enabled  bool  `json:"enabled"`
	Points   int64 `json:"points"`
	DailyCap int64 `json:"dailyCap"` // gross awards; reversals do not reopen a cap
	Reverse  bool  `json:"reverse"`
}
type ForumMembership struct {
	ForumID      int64    `json:"forumId,string"`
	MinimumLevel int      `json:"minimumLevel"`
	MembersOnly  bool     `json:"membersOnly"`
	Denied       []string `json:"denied"` // restrictive override only
}
type MembershipConfig struct {
	GuestPermissions map[string]bool       `json:"guestPermissions"`
	Version          int64                 `json:"version"`
	Levels           []MemberLevel         `json:"levels"`
	Rules            map[string]GrowthRule `json:"rules"`
	Forums           []ForumMembership     `json:"forums"`
}

func DefaultMembershipConfig() MembershipConfig {
	c := MembershipConfig{GuestPermissions: map[string]bool{"forum.read": true, "attachment.download": true}, Version: 1, Forums: []ForumMembership{}, Rules: map[string]GrowthRule{
		"active": {true, 1, 1, false}, "thread": {true, 5, 50, true}, "reply": {true, 2, 40, true}, "like": {true, 1, 20, true}, "digest": {true, 20, 100, true},
	}}
	for i, name := range []string{"新手会员", "正式会员", "活跃会员", "资深会员", "核心会员"} {
		p := map[string]bool{}
		for _, a := range MemberActions {
			p[a] = true
		}
		p["post.link.direct"] = i >= 1
		p["post.skip.moderate"] = i >= 2
		l := MemberLevel{ID: i, Name: name, Rank: i, Automatic: true, Experience: []int64{0, 100, 500, 1500, 5000}[i], Permissions: p,
			Limits: MemberLimits{ThreadsPerDay: 100, RepliesPerDay: 100, UploadsPerDay: 720, UploadBytesPerDay: -1, ImageBytes: -1, FileBytes: -1, AttachmentsPerPost: -1, EditMinutes: -1, SignatureLength: 300},
			Badge:  LevelBadge{Label: fmt.Sprintf("LV%d", i), Icon: []string{"seedling", "star", "shield", "gem", "crown"}[i], Color: "#334155", Background: "#e2e8f0"}}
		c.Levels = append(c.Levels, l)
	}
	return c
}

func (c MembershipConfig) Level(id int) (MemberLevel, bool) {
	for _, l := range c.Levels {
		if l.ID == id {
			return l, true
		}
	}
	return MemberLevel{}, false
}
func (c MembershipConfig) Forum(id int64) (ForumMembership, bool) {
	for _, f := range c.Forums {
		if f.ForumID == id {
			return f, true
		}
	}
	return ForumMembership{}, false
}

var colorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (c MembershipConfig) Validate() error {
	bad := func(s string) error { return fmt.Errorf("会员配置无效：%s", s) }
	if len(c.GuestPermissions) != 2 {
		return bad("须提供游客阅读及附件权限")
	}
	for _, a := range []string{"forum.read", "attachment.download"} {
		if _, ok := c.GuestPermissions[a]; !ok {
			return bad("游客权限不合法")
		}
	}
	if len(c.Levels) < 1 || len(c.Levels) > 50 || len(c.Forums) > 1000 {
		return bad("等级数量须为 1–50，版块规则最多 1000 条")
	}
	ids, ranks := map[int]bool{}, map[int]bool{}
	for _, l := range c.Levels {
		if l.ID < 0 || l.ID > 32767 || l.Rank < 0 || l.Rank > 10000 || ids[l.ID] || ranks[l.Rank] {
			return bad("等级 ID 和顺序必须唯一且在有效范围内")
		}
		ids[l.ID] = true
		ranks[l.Rank] = true
		if strings.TrimSpace(l.Name) == "" || len([]rune(l.Name)) > 30 || l.Experience < 0 || l.Experience > 1e12 || l.DaysVisited < 0 || l.DaysVisited > 36500 || l.PostsRead < 0 || l.PostsRead > 1e9 || l.PostCount < 0 || l.PostCount > 1e9 {
			return bad("等级名称或门槛越界")
		}
		if len(l.Permissions) != len(MemberActions) {
			return bad("须提供完整的会员权限矩阵")
		}
		for _, a := range MemberActions {
			if _, ok := l.Permissions[a]; !ok {
				return bad("未知或缺失的权限")
			}
		}
		b, _ := json.Marshal(l.Limits)
		var limits map[string]int64
		_ = json.Unmarshal(b, &limits)
		for _, v := range limits {
			if v < -1 || v > 1e12 {
				return bad("额度须为 -1（不限）、0（禁止）或正数")
			}
		}
		if !colorRE.MatchString(l.Badge.Color) || !colorRE.MatchString(l.Badge.Background) || len([]rune(l.Badge.Label)) > 20 {
			return bad("徽章颜色或标签不合法")
		}
		switch l.Badge.Icon {
		case "", "seedling", "star", "crown", "shield", "gem":
		default:
			return bad("不支持的徽章图标")
		}
	}
	base, ok := c.Level(0)
	if !ok || base.Rank != 0 || !base.Automatic || base.Experience != 0 || base.DaysVisited != 0 || base.PostsRead != 0 || base.PostCount != 0 || base.EmailVerified {
		return bad("默认等级 0 必须保留且无升级门槛")
	}
	if len(c.Rules) != 5 {
		return bad("成长规则类型必须完整")
	}
	for _, k := range []string{"active", "thread", "reply", "like", "digest"} {
		r, ok := c.Rules[k]
		if !ok || r.Points < 0 || r.Points > 100000 || r.DailyCap < 0 || r.DailyCap > 10000000 {
			return bad("成长规则分值或每日上限不合法")
		}
	}
	seen := map[int64]bool{}
	for _, f := range c.Forums {
		if f.ForumID <= 0 || !ids[f.MinimumLevel] || seen[f.ForumID] {
			return bad("版块或最低等级不合法")
		}
		seen[f.ForumID] = true
		for _, a := range f.Denied {
			found := false
			for _, p := range MemberActions {
				found = found || p == a
			}
			if !found {
				return bad("未知版块权限")
			}
		}
	}
	return nil
}

func (c *MembershipConfig) Normalize() {
	sort.Slice(c.Levels, func(i, j int) bool { return c.Levels[i].Rank < c.Levels[j].Rank })
	sort.Slice(c.Forums, func(i, j int) bool { return c.Forums[i].ForumID < c.Forums[j].ForumID })
}

var ErrMembershipConflict = errors.New("会员配置或用户状态已变化，请重新预览")
var ErrMemberQuota = errors.New("已达到会员等级额度")
