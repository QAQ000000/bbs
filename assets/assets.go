// SPDX-License-Identifier: AGPL-3.0-or-later
// Package assets 以 embed 方式打包模板、静态资源与数据库 schema，
// 编译产物为单二进制，部署即用。
package assets

import (
	"embed"
	"io/fs"
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
