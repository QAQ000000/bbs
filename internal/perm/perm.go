// Package perm 权限模型（单一权威清单）：
// 固定角色（会员/版主/管理员）→ 命名权限点的映射表集中于此，
// 所有权限判定必须经 Allowed() 查询本表，禁止在业务代码里散落角色判断。
// 演进路径：将来若需数据驱动（后台可调矩阵/自定义角色），
// 仅需把 rolePerms 换成 DB 读取，Allowed 的调用方零改动。
package perm

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
	AdminPanel       Point = "admin.panel"        // 进入管理后台
	ForumManage      Point = "forum.manage"       // 版块与分类管理
	ContentModerate  Point = "content.moderate"   // 内容治理（版主限其管辖版块）
	ContentEditOwn   Point = "content.edit.own"   // 编辑自己的楼层
	ContentEditAny   Point = "content.edit.any"   // 编辑任何人的楼层
	ContentDeleteOwn Point = "content.delete.own" // 删除自己的楼层
	ContentDeleteAny Point = "content.delete.any" // 删除任何人的楼层
	UserBan          Point = "user.ban"           // 禁言/解禁
	UserDelete       Point = "user.delete"        // 删号
	UserSetGroup     Point = "user.group"         // 调整用户组
	SettingsEdit     Point = "settings.edit"      // 站点设置
	CensorManage     Point = "censor.manage"      // 敏感词管理
	AnnounceManage   Point = "announce.manage"    // 公告管理
	LogsView         Point = "logs.view"          // 管理日志查看
	RecycleBin       Point = "recycle.bin"        // 回收站管理
	PruneRun         Point = "prune.run"          // 批量删帖
	ModerateQueue    Point = "moderate.queue"     // 审核队列
	UploadUse        Point = "upload.use"         // 使用本站上传
)

// TrustLevel 信任等级（对应 users.trust_level）。
type TrustLevel int

const (
	TrustNewUser TrustLevel = 0 // 新用户
	TrustMember  TrustLevel = 1 // 正式成员
	TrustSenior  TrustLevel = 2 // 资深成员
)

// PostLinkDirect 直接发含链接内容而不进审核队列；
// SkipModerate 资深成员（TL2）在全站发帖审核开启时免审核。
const (
	PostLinkDirect Point = "post.link.direct"
	SkipModerate   Point = "post.skip.moderate"
)

// rolePerms 角色 → 权限点映射（唯一权威清单）。
// 管理员拥有全部权限点；版主拥有内容治理面；会员拥有基础面。
var rolePerms = map[Role]map[Point]bool{
	RoleAdmin: {
		AdminPanel: true, ForumManage: true, ContentModerate: true,
		ContentEditOwn: true, ContentEditAny: true, ContentDeleteOwn: true, ContentDeleteAny: true,
		UserBan: true, UserDelete: true, UserSetGroup: true,
		SettingsEdit: true, CensorManage: true, AnnounceManage: true,
		LogsView: true, RecycleBin: true, PruneRun: true, ModerateQueue: true,
		UploadUse: true,
	},
	RoleModerator: {
		ContentModerate: true,
		ContentEditOwn:  true, ContentDeleteOwn: true, ContentDeleteAny: true,
		RecycleBin: true, PruneRun: true, ModerateQueue: true,
		UploadUse: true,
	},
	RoleMember: {
		ContentEditOwn: true, ContentDeleteOwn: true,
		UploadUse: true,
	},
}

// Allowed 判定角色是否拥有权限点。
func Allowed(r Role, p Point) bool {
	return rolePerms[r][p]
}

// TrustAllowed 判定信任等级是否拥有信任轴权限点。
func TrustAllowed(t TrustLevel, p Point) bool {
	switch p {
	case PostLinkDirect:
		return t >= TrustMember
	case SkipModerate:
		return t >= TrustSenior
	}
	return false
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
