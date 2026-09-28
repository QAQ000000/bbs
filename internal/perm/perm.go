// Package perm 权限模型（单一权威清单）：
// 固定角色（会员/版主/管理员）→ 命名权限点的映射表集中于此，
// 所有权限判定必须经 Allowed() 查询本表，禁止在业务代码里散落角色判断。
// 演进路径：将来若需数据驱动（后台可调矩阵/自定义角色），
// 仅需把 rolePerms 换成 DB 读取，Allowed 的调用方零改动。
package perm

import "sync"

// Role 用户组（对应 users.group_id）。
type Role int

const (
	RoleMember    Role = 0 // 会员
	RoleAdmin     Role = 1 // 管理员
	RoleModerator Role = 2 // 版主
)

// Point 命名权限点。
type Point string

const (
	EngagementView      Point = "engagement.view"
	EngagementConfigure Point = "engagement.configure"
	PollManage          Point = "polls.manage"
	BountyManage        Point = "bounties.manage"
	AdminPanel          Point = "admin.panel"        // 进入管理后台
	ForumManage         Point = "forum.manage"       // 版块与分类管理
	ContentModerate     Point = "content.moderate"   // 内容治理（版主限其管辖版块）
	ContentEditOwn      Point = "content.edit.own"   // 编辑自己的楼层
	ContentEditAny      Point = "content.edit.any"   // 编辑任何人的楼层
	ContentDeleteOwn    Point = "content.delete.own" // 删除自己的楼层
	ContentDeleteAny    Point = "content.delete.any" // 删除任何人的楼层
	UserBan             Point = "user.ban"           // 禁言/解禁
	UserDelete          Point = "user.delete"        // 删号
	UserSetGroup        Point = "user.group"         // 调整用户组
	SettingsEdit        Point = "settings.edit"      // 站点设置
	CensorManage        Point = "censor.manage"      // 敏感词管理
	AnnounceManage      Point = "announce.manage"    // 公告管理
	LogsView            Point = "logs.view"          // 管理日志查看
	RecycleBin          Point = "recycle.bin"        // 回收站管理
	PruneRun            Point = "prune.run"          // 批量删帖
	ModerateQueue       Point = "moderate.queue"     // 审核队列
	PermissionsEdit     Point = "permissions.edit"
	UsersView           Point = "users.view"
	MemberView          Point = "membership.view"
	MemberConfigure     Point = "membership.configure"
	MemberAdjust        Point = "membership.adjust"
	ExperienceAdjust    Point = "experience.adjust"
	MemberLogs          Point = "membership.logs"
	TitleView           Point = "titles.view"
	TitleConfigure      Point = "titles.configure"
	TitleGrant          Point = "titles.grant"
	TitleRevoke         Point = "titles.revoke"
	TitleLogs           Point = "titles.logs"
	TagsConfigure       Point = "tags.configure"
	EmailManage         Point = "email.manage"
	PointsView          Point = "points.view"
	PointsConfigure     Point = "points.configure"
	PointsAdjust        Point = "points.adjust"
	ReplyAccept         Point = "reply.accept"
	UploadUse           Point = "upload.use" // 使用本站上传
)

// rolePerms 角色 → 权限点映射（唯一权威清单）。
// 管理员拥有全部权限点；版主拥有内容治理面；会员拥有基础面。
var rolePerms = map[Role]map[Point]bool{
	RoleAdmin: {
		EngagementView: true, EngagementConfigure: true, PollManage: true, BountyManage: true,
		PointsView: true, PointsConfigure: true, PointsAdjust: true,
		EmailManage:   true,
		TagsConfigure: true,
		TitleView:     true, TitleConfigure: true, TitleGrant: true, TitleRevoke: true, TitleLogs: true, ReplyAccept: true,
		PermissionsEdit: true, UsersView: true, MemberView: true, MemberConfigure: true, MemberAdjust: true, ExperienceAdjust: true, MemberLogs: true,
		AdminPanel: true, ForumManage: true, ContentModerate: true,
		ContentEditOwn: true, ContentEditAny: true, ContentDeleteOwn: true, ContentDeleteAny: true,
		UserBan: true, UserDelete: true, UserSetGroup: true,
		SettingsEdit: true, CensorManage: true, AnnounceManage: true,
		LogsView: true, RecycleBin: true, PruneRun: true, ModerateQueue: true,
		UploadUse: true,
	},
	RoleModerator: {
		ReplyAccept:     true,
		ContentModerate: true,
		ContentEditOwn:  true, ContentDeleteOwn: true, ContentDeleteAny: true,
		RecycleBin: true, PruneRun: true, ModerateQueue: true,
		UploadUse: true,
	},
	RoleMember: {
		ReplyAccept:    true,
		ContentEditOwn: true, ContentDeleteOwn: true,
		UploadUse: true,
	},
}

// matrix 当前生效的权限矩阵：默认取编译期 rolePerms，后台保存后由
// store 调 Load 整表替换。AdminPanel×管理员为硬保护（防自锁，不受 DB 覆盖）。
var matrixMu sync.RWMutex
var matrix = rolePerms

// Allowed 判定角色是否拥有权限点（单点查询，全站唯一入口）。
func Allowed(r Role, p Point) bool {
	matrixMu.RLock()
	defer matrixMu.RUnlock()
	if r == RoleAdmin && (p == AdminPanel || p == PermissionsEdit) {
		return true // 硬保护
	}
	return matrix[r][p]
}

// Load 用 DB 中的矩阵整表替换运行时值（缺省权限点回退编译期默认）。
func Load(m map[Role]map[Point]bool) {
	merged := map[Role]map[Point]bool{}
	for role, defs := range rolePerms {
		merged[role] = map[Point]bool{}
		for pt, allowed := range defs {
			merged[role][pt] = allowed
		}
	}
	for role, pts := range m {
		if _, ok := merged[role]; !ok {
			merged[role] = map[Point]bool{}
		}
		for pt, allowed := range pts {
			merged[role][pt] = allowed
		}
	}
	matrixMu.Lock()
	matrix = merged
	matrixMu.Unlock()
}

// Matrix 返回当前矩阵（后台矩阵页渲染用，副本）。
func Matrix() map[Role]map[Point]bool {
	matrixMu.RLock()
	defer matrixMu.RUnlock()
	out := map[Role]map[Point]bool{}
	for role, pts := range matrix {
		out[role] = map[Point]bool{}
		for pt, allowed := range pts {
			out[role][pt] = allowed
		}
	}
	return out
}

// AllPoints 全部命名权限点（矩阵页按此顺序渲染）。
func AllPoints() []Point {
	return []Point{
		EngagementView, EngagementConfigure, PollManage, BountyManage,
		PointsView, PointsConfigure, PointsAdjust,
		EmailManage,
		TagsConfigure,
		TitleView, TitleConfigure, TitleGrant, TitleRevoke, TitleLogs, ReplyAccept,
		PermissionsEdit, UsersView, MemberView, MemberConfigure, MemberAdjust, ExperienceAdjust, MemberLogs,
		AdminPanel, ForumManage, ContentModerate,
		ContentEditOwn, ContentEditAny, ContentDeleteOwn, ContentDeleteAny,
		UserBan, UserDelete, UserSetGroup,
		SettingsEdit, CensorManage, AnnounceManage,
		LogsView, RecycleBin, PruneRun, ModerateQueue,
		UploadUse,
	}
}

// Defaults 返回编译期默认矩阵（权限表播种用，副本）。
func Defaults() map[Role]map[Point]bool {
	out := map[Role]map[Point]bool{}
	for role, pts := range rolePerms {
		out[role] = map[Point]bool{}
		for pt, allowed := range pts {
			out[role][pt] = allowed
		}
	}
	return out
}

// RoleFromGroupID users.group_id → Role（未知值按会员处理）。
func RoleFromGroupID(gid int) Role {
	switch gid {
	case 1:
		return RoleAdmin
	case 2:
		return RoleModerator
	}
	return RoleMember
}

// RoleName 角色显示名。
func RoleName(r Role) string {
	switch r {
	case RoleAdmin:
		return "管理员"
	case RoleModerator:
		return "版主"
	}
	return "会员"
}
