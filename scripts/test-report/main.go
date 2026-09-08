// test-report rejects incomplete, skipped or failing go test -json runs.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "validate-databases" {
		seen := map[string]bool{}
		for _, key := range []string{"FORUM_STORE_TEST_DSN", "FORUM_API_TEST_DSN", "FORUM_MIGRATION_TEST_DSN"} {
			if os.Getenv(key) == "" {
				return fmt.Errorf("%s is required", key)
			}
			cfg, err := pgxpool.ParseConfig(os.Getenv(key))
			if err != nil {
				return fmt.Errorf("%s is not a valid DSN", key)
			}
			name := cfg.ConnConfig.Database
			if !strings.HasPrefix(name, "gobbs_test_") || seen[name] {
				return fmt.Errorf("test database names must be distinct and start with gobbs_test_")
			}
			seen[name] = true
		}
		return nil
	}
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: test-report results.json package...")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	passed := map[string]bool{}
	tests := map[string]int{}
	for {
		var e struct{ Action, Package, Test, Output string }
		if err := decoder.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if e.Action == "skip" || e.Action == "fail" || strings.Contains(e.Output, "SKIP:") {
			return fmt.Errorf("test gate rejected %s %s: %s", e.Package, e.Test, e.Action)
		}
		if e.Action == "pass" {
			if e.Test == "" {
				passed[e.Package] = true
			} else {
				tests[e.Package]++
			}
		}
	}
	for _, pkg := range os.Args[2:] {
		if !passed[pkg] || tests[pkg] == 0 {
			return fmt.Errorf("%s did not execute and pass tests", pkg)
		}
		fmt.Printf("%s: %d tests passed, no skips\n", pkg, tests[pkg])
	}
	return nil
}
