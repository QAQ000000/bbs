// SPDX-License-Identifier: AGPL-3.0-or-later
// Package smiley 表情系统：
//   - 内置一组 Unicode emoji 短代码（:smile: 等），渲染为字符本身，零图片资产；
//   - 支持从 data/smiley/ 加载站点自定义图片表情包（如从旧论坛导入），
//     每个包一个 <包名>.json 描述 + <包名>/ 图片目录，运行时按目录变更热加载。
//
// 本包不包含任何第三方论坛的素材；自定义包由站点管理员自行导入并对其合法性负责。
package smiley

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Code 一个表情：Unicode 非空表示内置 emoji（渲染为字符），
// File 非空表示图片表情（渲染为 <img>，URL 为 /smiley/<Pkg>/<File>）。
type Code struct {
	Code    string `json:"code"`
	Pkg     string `json:"pkg,omitempty"`
	File    string `json:"file,omitempty"`
	Unicode string `json:"unicode,omitempty"`
}

// Package 图片表情包描述。
type Package struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

var (
	customMu       sync.RWMutex
	customCodes    []Code
	customPkgs     []Package
	customByCode   map[string]Code
	customLoadedAt time.Time
	customDir      string
)

// LoadCustom 从 dir 加载自定义图片表情包（启动时与目录变更后调用）。
func LoadCustom(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var pkgs []Package
	var codes []Code
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pkg struct {
			Name  string `json:"name"`
			Dir   string `json:"dir"`
			Codes []Code `json:"codes"`
		}
		if json.Unmarshal(b, &pkg) != nil || pkg.Dir == "" {
			continue
		}
		// 仅收录磁盘上真实存在的图片
		for _, c := range pkg.Codes {
			if c.File == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, pkg.Dir, c.File)); err != nil {
				continue
			}
			c.Pkg = pkg.Dir
			codes = append(codes, c)
		}
		pkgs = append(pkgs, Package{Name: pkg.Name, Dir: pkg.Dir})
	}

	customMu.Lock()
	customDir = dir
	customCodes, customPkgs = codes, pkgs
	customByCode = make(map[string]Code, len(codes))
	for _, c := range codes {
		if _, dup := customByCode[c.Code]; !dup {
			customByCode[c.Code] = c
		}
	}
	customLoadedAt = time.Now()
	customMu.Unlock()
	return nil
}

// maybeReload 目录内容变化时自动重载（开发导入后无需重启）。
func maybeReload() {
	customMu.RLock()
	dir, loaded := customDir, customLoadedAt
	customMu.RUnlock()
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var newest time.Time
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.After(loaded.Add(time.Second)) {
		_ = LoadCustom(dir)
	}
}

// Match 返回 s 从头开始能匹配到的最长表情代码；优先自定义图片包，其次内置 emoji。
// 供 Markdown 内联解析器调用。
func Match(s string) (Code, int, bool) {
	r := firstRune(s)
	if r == 0 {
		return Code{}, 0, false
	}
	if r == ':' || r == '{' {
		maybeReload()
		customMu.RLock()
		var best Code
		bestLen := 0
		for code, c := range customByCode {
			if len(code) > bestLen && strings.HasPrefix(s, code) {
				best, bestLen = c, len(code)
			}
		}
		customMu.RUnlock()
		for code, c := range builtinByCode {
			if len(code) > bestLen && strings.HasPrefix(s, code) && codeEnds(s, len(code)) {
				best, bestLen = c, len(code)
			}
		}
		if bestLen > 0 {
			return best, bestLen, true
		}
	}
	return Code{}, 0, false
}

// codeEnds 检查内置短代码在 s 中截止处是否为词边界（避免 :grin:xx 之类误配后缀）。
func codeEnds(s string, n int) bool {
	if n >= len(s) {
		return true
	}
	next, _ := nextRune(s[n:])
	return !isCodeRune(next)
}

func isCodeRune(r rune) bool {
	return r == '_' || r == '-' || r == '+' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func nextRune(s string) (rune, int) {
	for i, r := range s {
		return r, i
	}
	return 0, 0
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// Groups 返回编辑器面板分组：内置 emoji 在前，自定义图片包按名称随后。
func Groups() []Group {
	maybeReload()
	customMu.RLock()
	pkgs := append([]Package(nil), customPkgs...)
	codes := append([]Code(nil), customCodes...)
	customMu.RUnlock()

	var out []Group
	out = append(out, Group{Name: "Emoji", Dir: "emoji", Codes: builtinList})
	byPkg := map[string][]Code{}
	for _, c := range codes {
		byPkg[c.Pkg] = append(byPkg[c.Pkg], c)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	for _, p := range pkgs {
		g := Group{Name: p.Name, Dir: p.Dir, Codes: byPkg[p.Dir]}
		if g.Codes != nil {
			out = append(out, g)
		}
	}
	return out
}

// Group 编辑器面板的一组表情。
type Group struct {
	Name  string
	Dir   string
	Codes []Code
}

// URL 图片表情的站点路径（内置 emoji 不产生 URL）。
func URL(c Code) string {
	if c.File == "" {
		return ""
	}
	return "/smiley/" + c.Pkg + "/" + c.File
}
