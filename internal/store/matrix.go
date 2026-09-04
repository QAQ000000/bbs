// SPDX-License-Identifier: AGPL-3.0-or-later
// store/matrix.go：角色权限矩阵的持久化与运行时同步（ROADMAP P1-6）。
// 空表按编译期默认矩阵播种；后台保存后立即同步 perm 运行时。
package store

import (
	"context"

	"dzforum/internal/perm"
)

// SeedRolePermsIfEmpty 表为空时按默认矩阵播种（启动时调用，幂等）。
func (s *Store) SeedRolePermsIfEmpty(ctx context.Context) error {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM role_perms`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return s.saveRolePerms(ctx, perm.Defaults())
}

// LoadRolePerms 读取 DB 矩阵并同步到 perm 运行时（启动时调用）。
func (s *Store) LoadRolePerms(ctx context.Context) error {
	if err := s.SeedRolePermsIfEmpty(ctx); err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT role_id, point, allowed FROM role_perms`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[perm.Role]map[perm.Point]bool{}
	for rows.Next() {
		var rid int
		var pt string
		var allowed bool
		if err := rows.Scan(&rid, &pt, &allowed); err != nil {
			return err
		}
		role, point := perm.Role(rid), perm.Point(pt)
		if m[role] == nil {
			m[role] = map[perm.Point]bool{}
		}
		m[role][point] = allowed
	}
	if err := rows.Err(); err != nil {
		return err
	}
	perm.Load(m)
	return nil
}

// SaveRolePerms 覆盖写矩阵并立即同步运行时（后台矩阵页保存）。
func (s *Store) SaveRolePerms(ctx context.Context, m map[perm.Role]map[perm.Point]bool) error {
	if err := s.saveRolePerms(ctx, m); err != nil {
		return err
	}
	perm.Load(m)
	return nil
}

// saveRolePerms 单语句数组参数写入（三列 unnest，全量绑定）。
func (s *Store) saveRolePerms(ctx context.Context, m map[perm.Role]map[perm.Point]bool) error {
	var rids []int16
	var pts []string
	var allowed []bool
	for role, pset := range m {
		for pt, a := range pset {
			rids = append(rids, int16(role))
			pts = append(pts, string(pt))
			allowed = append(allowed, a)
		}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO role_perms (role_id, point, allowed)
		SELECT r, p, a FROM unnest($1::smallint[], $2::text[], $3::boolean[]) AS t(r, p, a)
		ON CONFLICT (role_id, point) DO UPDATE SET allowed = EXCLUDED.allowed`,
		rids, pts, allowed)
	return err
}
