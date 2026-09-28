// SPDX-License-Identifier: AGPL-3.0-or-later
// gobbsctl：GoBBS 的小型发布与运行检查工具。
package main

import (
	"context"
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
	fmt.Fprintln(os.Stderr, "用法: gobbsctl version | status [-url URL] | upgrade -binary PATH [-service gobbs] [-url URL] [-backup-dir DIR] [-timeout 60s] [-command-timeout 30s] [-request-timeout 5s]")
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "gobbsctl:", err); os.Exit(1) }

type options struct {
	binary, service, url, backupDir         string
	timeout, commandTimeout, requestTimeout time.Duration
}

func parseOptions(args []string) (options, error) {
	f := flag.NewFlagSet("gobbsctl", flag.ContinueOnError)
	o := options{}
	f.StringVar(&o.binary, "binary", "", "新 forumd 二进制路径，须与目标 forumd 同目录")
	f.StringVar(&o.service, "service", "gobbs", "systemd 服务名")
	f.StringVar(&o.url, "url", "http://127.0.0.1:8090/api/status", "健康检查 URL")
	f.StringVar(&o.backupDir, "backup-dir", "", "旧二进制备份目录，默认目标目录下 releases")
	f.DurationVar(&o.timeout, "timeout", 60*time.Second, "每次启动后的健康等待上限")
	f.DurationVar(&o.commandTimeout, "command-timeout", 30*time.Second, "单次 systemctl 命令时限")
	f.DurationVar(&o.requestTimeout, "request-timeout", 5*time.Second, "单次健康请求时限")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 || o.timeout <= 0 || o.commandTimeout <= 0 || o.requestTimeout <= 0 || strings.TrimSpace(o.service) == "" || strings.HasPrefix(o.service, "-") {
		return o, errors.New("参数无效：不接受额外位置参数，超时必须为正，服务名不能为空或以连字符开头")
	}
	return o, nil
}

func runStatus(args []string) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	if err = health(ctx, o.url, o.requestTimeout); err != nil {
		return err
	}
	fmt.Println("服务正常:", strings.TrimSuffix(o.url, "/"))
	return nil
}

func runUpgrade(args []string) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	service, err := exec.LookPath("systemctl")
	if err != nil {
		return fmt.Errorf("systemctl 不可用: %w", err)
	}
	return upgrade(o, service, command)
}

type commandRunner func(context.Context, string, ...string) error

func serviceCommand(o options, bin string, run commandRunner, action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), o.commandTimeout)
	defer cancel()
	if err := run(ctx, bin, action, "--", o.service); err != nil {
		return fmt.Errorf("systemctl %s: %w", action, err)
	}
	return nil
}

func upgrade(o options, service string, run commandRunner) error {
	if o.binary == "" {
		return errors.New("必须指定 -binary")
	}
	newPath, err := filepath.Abs(o.binary)
	if err != nil {
		return err
	}
	if err = checkExecutable(newPath); err != nil {
		return err
	}
	target := filepath.Join(filepath.Dir(newPath), "forumd")
	if newPath == target {
		return errors.New("-binary 不能是目标 forumd")
	}
	if err = checkExecutable(target); err != nil {
		return fmt.Errorf("当前二进制无效: %w", err)
	}
	backupDir := o.backupDir
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(target), "releases")
	}
	if err = os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}
	// Copy first: the current executable stays in place until atomic replacement.
	old, err := copyExecutable(target, backupDir, "forumd-"+time.Now().Format("20060102-150405")+"-*")
	if err != nil {
		return fmt.Errorf("备份旧二进制: %w", err)
	}
	if err = os.Rename(newPath, target); err != nil {
		return fmt.Errorf("替换二进制失败，原文件仍保留（备份 %s）: %w", old, err)
	}
	err = serviceCommand(o, service, run, "restart")
	if err == nil {
		err = waitHealthy(o)
	}
	if err == nil {
		fmt.Printf("升级成功: %s\n旧二进制备份: %s\n", target, old)
		return nil
	}
	cause := err
	// Every post-replacement failure uses the same recovery path. Never remove the
	// backup: restoring the executable does not roll back database migrations.
	stopErr := serviceCommand(o, service, run, "stop")
	restored, restoreErr := copyExecutable(old, filepath.Dir(target), ".forumd-restore-*")
	if restoreErr == nil {
		restoreErr = os.Rename(restored, target)
		if restoreErr != nil {
			_ = os.Remove(restored)
		}
	}
	if restoreErr != nil {
		return fmt.Errorf("升级失败（%v），恢复旧文件失败，备份 %s: %w", cause, old, errors.Join(stopErr, restoreErr))
	}
	restartErr := serviceCommand(o, service, run, "restart")
	if restartErr != nil {
		return fmt.Errorf("升级失败（%v），旧文件已恢复但服务重启失败；备份 %s: %w", cause, old, errors.Join(stopErr, restartErr))
	}
	if healthErr := waitHealthy(o); healthErr != nil {
		return fmt.Errorf("升级失败（%v），旧文件已恢复但健康检查未通过；数据库迁移未回退，备份 %s: %w", cause, old, errors.Join(stopErr, healthErr))
	}
	if stopErr != nil {
		return fmt.Errorf("升级失败（%v），已恢复旧文件并通过健康检查，但停止服务曾失败: %w", cause, stopErr)
	}
	return fmt.Errorf("升级失败，已恢复旧二进制并通过健康检查（数据库迁移不会自动回退，备份 %s）: %w", old, cause)
}

func copyExecutable(source, dir, pattern string) (path string, err error) {
	src, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return "", err
	}
	dst, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = dst.Name()
	defer func() {
		_ = dst.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = io.Copy(dst, src); err != nil {
		return path, err
	}
	if err = dst.Chmod(st.Mode().Perm()); err != nil {
		return path, err
	}
	if err = dst.Sync(); err != nil {
		return path, err
	}
	err = dst.Close()
	return path, err
}

func checkExecutable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
		return fmt.Errorf("不是可执行普通文件: %s", path)
	}
	return nil
}

func command(ctx context.Context, bin string, args ...string) error {
	c := exec.CommandContext(ctx, bin, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.WaitDelay = time.Second
	err := c.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func health(ctx context.Context, url string, timeout time.Duration) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: timeout}
	r, err := client.Do(req)
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
	if err = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v); err != nil {
		return err
	}
	if !v.OK || v.DB != "up" {
		return fmt.Errorf("health not ready: ok=%v db=%s", v.OK, v.DB)
	}
	return nil
}

func waitHealthy(o options) error {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	for {
		err := health(ctx, o.url, o.requestTimeout)
		if err == nil {
			return nil
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("健康等待超时（最后错误 %v）: %w", err, ctx.Err())
		case <-timer.C:
		}
	}
}
