// SPDX-License-Identifier: AGPL-3.0-or-later
// Package assets 以 embed 方式打包模板、静态资源与数据库 schema，
// 编译产物为单二进制，部署即用。
package assets

import (
	"embed"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed templates static db
var content embed.FS

// Static 返回 static/ 子目录（css/js/表情图等）。
func Static() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// Templates 返回 templates/ 子目录。
func Templates() fs.FS {
	sub, err := fs.Sub(content, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// SchemaFile 返回初始 schema 的内容。
func SchemaFile() []byte {
	b, err := content.ReadFile("db/schema.sql")
	if err != nil {
		panic(err)
	}
	return b
}

// Migration 编号迁移（db/migrations/NNN_*.sql）。
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations 返回编号迁移列表（版本升序）。
// 约定：schema.sql 保持「当前完整形态」的幂等 DDL，供全新库初始化；
// 存量库通过编号迁移演进（版本 1 为 schema.sql 基线，不入目录）。
// 每个迁移在全新库（schema.sql 已是最终形态）上重复执行时必须幂等或无害。
func Migrations() []Migration {
	entries, err := fs.ReadDir(content, "db/migrations")
	if err != nil {
		return nil
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		i := strings.IndexByte(base, '_')
		if i <= 0 {
			continue
		}
		v, err := strconv.Atoi(base[:i])
		if err != nil || v <= 1 {
			continue
		}
		b, err := content.ReadFile("db/migrations/" + e.Name())
		if err != nil {
			panic(err)
		}
		out = append(out, Migration{Version: v, Name: e.Name(), SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// MigrationSQL 返回指定版本的迁移内容；版本不存在返回空串。
func MigrationSQL(version int) string {
	for _, m := range Migrations() {
		if m.Version == version {
			return m.SQL
		}
	}
	return ""
}
