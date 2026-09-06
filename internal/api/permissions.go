// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"dzforum/internal/perm"
	"dzforum/internal/store"
)

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
