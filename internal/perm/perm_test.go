// SPDX-License-Identifier: AGPL-3.0-or-later

package perm

import "testing"

// TestRoleMatrix 角色→权限点全矩阵断言（权限表的可执行文档）。
// 修改权限设计时同步更新本表。
func TestRoleMatrix(t *testing.T) {
	all := []Point{
		AdminPanel, ForumManage, ContentModerate,
		ContentEditOwn, ContentEditAny, ContentDeleteOwn, ContentDeleteAny,
		UserBan, UserDelete, UserSetGroup,
		SettingsEdit, CensorManage, AnnounceManage,
		LogsView, RecycleBin, PruneRun, ModerateQueue, UploadUse,
	}
	expect := map[Role]map[Point]bool{
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
	for _, role := range []Role{RoleAdmin, RoleModerator, RoleMember} {
		for _, p := range all {
			got, want := Allowed(role, p), expect[role][p]
			if got != want {
				t.Errorf("%s × %s = %v，期望 %v", RoleName(role), p, got, want)
			}
		}
	}
}

func TestRoleFromGroupID(t *testing.T) {
	cases := map[int]Role{0: RoleMember, 1: RoleAdmin, 2: RoleModerator, 9: RoleMember}
	for gid, want := range cases {
		if got := RoleFromGroupID(gid); got != want {
			t.Fatalf("group %d → %v，期望 %v", gid, got, want)
		}
	}
}
