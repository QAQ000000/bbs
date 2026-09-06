// SPDX-License-Identifier: AGPL-3.0-or-later
// gobbsctl：GoBBS 的小型发布与运行检查工具。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "status":
		if err := runStatus(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "upgrade":
		if err := runUpgrade(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: gobbsctl version | status [-url URL] | upgrade -binary PATH [-service gobbs] [-url URL] [-backup-dir DIR]")
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "gobbsctl:", err); os.Exit(1) }

type options struct {
	binary, service, url, backupDir string
	timeout                         time.Duration
}

func parseOptions(args []string) options {
	f := flag.NewFlagSet("gobbsctl", flag.ContinueOnError)
	o := options{}
	f.StringVar(&o.binary, "binary", "", "新 forumd 二进制路径")
	f.StringVar(&o.service, "service", "gobbs", "systemd 服务名")
	f.StringVar(&o.url, "url", "http://127.0.0.1:8090/api/status", "健康检查 URL")
	f.StringVar(&o.backupDir, "backup-dir", "", "旧二进制备份目录，默认与新二进制同目录")
	f.DurationVar(&o.timeout, "timeout", 60*time.Second, "重启后健康检查超时")
	_ = f.Parse(args)
	return o
}

func runStatus(args []string) error {
	o := parseOptions(args)
	return printHealth(o.url)
}

func runUpgrade(args []string) error {
	o := parseOptions(args)
	if o.binary == "" {
		return errors.New("必须指定 -binary")
	}
	newPath, err := filepath.Abs(o.binary)
	if err != nil {
		return err
	}
	if err := checkExecutable(newPath); err != nil {
		return err
	}
	service, err := exec.LookPath("systemctl")
	if err != nil {
		return fmt.Errorf("systemctl 不可用: %w", err)
	}
	target := filepath.Join(filepath.Dir(newPath), "forumd")
	if target == newPath {
		return errors.New("-binary 不能是正在运行的 forumd")
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("当前二进制不存在 %s: %w", target, err)
	}
	backupDir := o.backupDir
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(target), "releases")
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	old := filepath.Join(backupDir, "forumd-"+stamp)
	if err := os.Rename(target, old); err != nil {
		return fmt.Errorf("保存旧二进制: %w", err)
	}
	replaced := false
	defer func() {
		if !replaced {
			_ = os.Rename(old, target)
		}
	}()
	if err := os.Rename(newPath, target); err != nil {
		return fmt.Errorf("替换二进制: %w", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return err
	}
	if err := command(service, "restart", o.service); err != nil {
		return fmt.Errorf("重启服务: %w", err)
	}
	deadline := time.Now().Add(o.timeout)
	var last error
	for time.Now().Before(deadline) {
		if err := health(o.url); err == nil {
			replaced = true
			fmt.Printf("升级成功: %s\n旧二进制备份: %s\n", target, old)
			return nil
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	// 健康检查失败：恢复旧文件并重启，旧版本仍可用时才报告回滚成功。
	_ = command(service, "stop", o.service)
	_ = os.Remove(target)
	if err := os.Rename(old, target); err != nil {
		return fmt.Errorf("健康检查失败，且恢复旧二进制失败: %w", err)
	}
	if err := command(service, "start", o.service); err != nil {
		return fmt.Errorf("已恢复旧二进制，但启动失败: %w", err)
	}
	return fmt.Errorf("新版本健康检查失败，已恢复旧二进制: %w", last)
}

func checkExecutable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("不是文件: %s", path)
	}
	if st.Mode()&0o111 == 0 {
		return fmt.Errorf("文件不可执行: %s", path)
	}
	return nil
}

func command(bin string, args ...string) error {
	c := exec.Command(bin, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func health(url string) error {
	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("health HTTP %d", r.StatusCode)
	}
	var v struct {
		OK bool   `json:"ok"`
		DB string `json:"db"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v); err != nil {
		return err
	}
	if !v.OK || v.DB != "up" {
		return fmt.Errorf("health not ready: ok=%v db=%s", v.OK, v.DB)
	}
	return nil
}

func printHealth(url string) error {
	if err := health(url); err != nil {
		return err
	}
	fmt.Println("服务正常:", strings.TrimSuffix(url, "/"))
	return nil
}
