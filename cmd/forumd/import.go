// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// importSmileys 从经典论坛的表情目录导入图片表情包（导入到站点自定义表情目录后退出）。
// 目录结构预期：<srcDir>/<包名>/<图片文件>。表情代码默认取图片文件名（去扩展名），
// 若提供 codesFile（由 scripts/gen_smileys.php 在你自己的论坛环境导出的映射 JSON），
// 则优先使用原始代码。
//
// 仅处理站点管理员本地已有的素材；本程序不附带、也不下载任何第三方论坛的图片资源。
func importSmileys(srcDir, outDir, codesFile string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("读取源目录: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// 可选的原始代码映射：pkg/file -> code
	codesMap := map[string]string{}
	if codesFile != "" {
		b, err := os.ReadFile(codesFile)
		if err != nil {
			return fmt.Errorf("读取代码映射: %w", err)
		}
		var data struct {
			Codes []struct {
				Code string `json:"code"`
				Pkg  string `json:"pkg"`
				File string `json:"file"`
			} `json:"codes"`
		}
		if err := json.Unmarshal(b, &data); err != nil {
			return fmt.Errorf("解析代码映射: %w", err)
		}
		for _, c := range data.Codes {
			codesMap[c.Pkg+"/"+c.File] = c.Code
		}
	}

	isImage := func(name string) bool {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".gif", ".png", ".jpg", ".jpeg", ".webp":
			return true
		}
		return false
	}

	imported := 0
	for _, pkgDir := range entries {
		if !pkgDir.IsDir() || strings.HasPrefix(pkgDir.Name(), ".") {
			continue
		}
		pkg := pkgDir.Name()
		files, err := os.ReadDir(filepath.Join(srcDir, pkg))
		if err != nil {
			continue
		}
		outPkg := filepath.Join(outDir, pkg)
		if err := os.MkdirAll(outPkg, 0o755); err != nil {
			return err
		}
		var codes []map[string]string
		for _, f := range files {
			if f.IsDir() || !isImage(f.Name()) {
				continue
			}
			in := filepath.Join(srcDir, pkg, f.Name())
			out := filepath.Join(outPkg, f.Name())
			if err := copyFileContents(in, out); err != nil {
				return fmt.Errorf("复制 %s: %w", in, err)
			}
			code := codesMap[pkg+"/"+f.Name()]
			if code == "" {
				code = ":" + strings.TrimSuffix(f.Name(), filepath.Ext(f.Name())) + ":"
			}
			codes = append(codes, map[string]string{"code": code, "file": f.Name()})
			imported++
		}
		if len(codes) == 0 {
			_ = os.RemoveAll(outPkg)
			continue
		}
		meta := map[string]any{"name": pkg, "dir": pkg, "codes": codes}
		b, _ := json.MarshalIndent(meta, "", "  ")
		if err := os.WriteFile(filepath.Join(outDir, pkg+".json"), b, 0o644); err != nil {
			return err
		}
		fmt.Printf("  包 %q：%d 个表情\n", pkg, len(codes))
	}

	if imported == 0 {
		return fmt.Errorf("在 %s 下没有找到可导入的表情图片", srcDir)
	}
	fmt.Printf("共导入 %d 个表情到 %s，重启服务后生效\n", imported, outDir)
	return nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
