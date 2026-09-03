// SPDX-License-Identifier: AGPL-3.0-or-later

package smiley

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchBuiltin(t *testing.T) {
	c, n, ok := Match(":smile: end")
	if !ok || c.Unicode != "😄" || n != len(":smile:") {
		t.Fatalf("应匹配内置 emoji: %+v %d %v", c, n, ok)
	}
	// 词边界：:grin: 后跟字母不整体匹配（当前实现为前缀精确匹配，:grinning: 更长优先）
	if c, n, ok := Match(":grinning_face_with_smiling_eyes"); ok && c.Code == ":grin:" {
		t.Fatalf("长代码应优先于其前缀: %s %d", c.Code, n)
	}
	// 未知代码
	if _, _, ok := Match(":totally_unknown:"); ok {
		t.Fatal("未知代码不应匹配")
	}
	// 非表情前缀
	if _, _, ok := Match("hello world"); ok {
		t.Fatal("非 :/{ 开头不应匹配")
	}
}

func TestLoadCustomAndMatch(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "mypack")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "hello.gif"), []byte("GIF89a-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{"name":"我的包","dir":"mypack","codes":[{"code":":hello:","file":"hello.gif"}]}`
	if err := os.WriteFile(filepath.Join(dir, "mypack.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadCustom(dir); err != nil {
		t.Fatalf("LoadCustom: %v", err)
	}
	c, n, ok := Match(":hello:")
	if !ok || c.File != "hello.gif" || c.Pkg != "mypack" || n != len(":hello:") {
		t.Fatalf("自定义图片表情应匹配: %+v %v", c, ok)
	}
	if u := URL(c); u != "/smiley/mypack/hello.gif" {
		t.Fatalf("URL 不正确: %s", u)
	}
	// 磁盘上不存在的图片应被剔除
	bad := `{"name":"bad","dir":"bad","codes":[{"code":":ghost:","file":"ghost.gif"}]}`
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = LoadCustom(dir)
	if _, _, ok := Match(":ghost:"); ok {
		t.Fatal("磁盘不存在的图片表情不应匹配")
	}

	groups := Groups()
	if len(groups) == 0 || groups[0].Name != "Emoji" {
		t.Fatalf("Groups 应含内置 Emoji 组: %+v", groups)
	}
	found := false
	for _, g := range groups {
		if g.Dir == "mypack" {
			found = true
		}
	}
	if !found {
		t.Fatal("Groups 应包含自定义包 mypack")
	}
}
