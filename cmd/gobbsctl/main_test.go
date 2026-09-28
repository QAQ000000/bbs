package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUpgradeRecovery(t *testing.T) {
	for _, scenario := range []string{"success", "restart_failure", "new_unhealthy", "both_unhealthy"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			target, next := filepath.Join(dir, "forumd"), filepath.Join(dir, "forumd.new")
			for path, content := range map[string]string{target: "old", next: "new"} {
				if err := os.WriteFile(path, []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			checks := map[string]int{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := os.ReadFile(target)
				if err != nil {
					http.Error(w, "missing", 500)
					return
				}
				checks[string(b)]++
				if scenario == "both_unhealthy" || scenario == "new_unhealthy" && string(b) == "new" {
					http.Error(w, "unhealthy", 503)
					return
				}
				_, _ = w.Write([]byte("{\"ok\":true,\"db\":\"up\"}"))
			}))
			defer srv.Close()
			o := options{binary: next, service: "isolated-test", url: srv.URL, timeout: 40 * time.Millisecond, commandTimeout: time.Second, requestTimeout: 20 * time.Millisecond}
			var calls []string
			err := upgrade(o, "test-systemctl", func(ctx context.Context, bin string, args ...string) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("missing command deadline")
				}
				if len(args) != 3 || args[1] != "--" || args[2] != o.service {
					t.Fatalf("bad command: %v", args)
				}
				calls = append(calls, args[0])
				if scenario == "restart_failure" && len(calls) == 1 {
					return errors.New("restart failed")
				}
				return nil
			})
			want, wantCalls := "new", []string{"restart"}
			if scenario == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				want, wantCalls = "old", []string{"restart", "stop", "restart"}
				if err == nil {
					t.Fatal("failed upgrade must return error even after recovery")
				}
				if scenario == "both_unhealthy" && !strings.Contains(err.Error(), "健康检查未通过") {
					t.Fatal(err)
				}
				if scenario != "both_unhealthy" && !strings.Contains(err.Error(), "通过健康检查") {
					t.Fatal(err)
				}
				if checks["old"] == 0 {
					t.Fatal("recovered service was not checked")
				}
			}
			b, _ := os.ReadFile(target)
			if string(b) != want || !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("binary=%q calls=%v", b, calls)
			}
			backups, _ := filepath.Glob(filepath.Join(dir, "releases", "forumd-*"))
			if len(backups) != 1 {
				t.Fatalf("backups=%v", backups)
			}
			b, _ = os.ReadFile(backups[0])
			if string(b) != "old" {
				t.Fatal("backup not preserved")
			}
			leftovers, _ := filepath.Glob(filepath.Join(dir, ".forumd-restore-*"))
			if len(leftovers) != 0 {
				t.Fatalf("temporary files: %v", leftovers)
			}
		})
	}
}

func TestHealthTimeoutAndInvalidOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	start := time.Now()
	if err := health(context.Background(), srv.URL, 20*time.Millisecond); err == nil || time.Since(start) > time.Second {
		t.Fatalf("individual HTTP timeout: %v", err)
	}
	start = time.Now()
	err := waitHealthy(options{url: srv.URL, timeout: 60 * time.Millisecond, requestTimeout: 20 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("unbounded wait: %v", err)
	}
	for _, args := range [][]string{{"-timeout=0"}, {"-command-timeout=-1s"}, {"-request-timeout=0"}, {"-unknown"}, {"extra"}, {"-service=--help"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("accepted invalid options: %v", args)
		}
	}
}
