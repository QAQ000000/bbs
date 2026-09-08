package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGateRejectsMissingSkippedOrIncompleteRuns(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	for _, tc := range []struct {
		name, events string
		ok           bool
	}{
		{"empty", "", false},
		{"no-tests", `{"Action":"pass","Package":"p"}`, false},
		{"skipped", `{"Action":"skip","Package":"p","Test":"T"}`, false},
		{"silent-store-skip", `{"Action":"output","Package":"p","Output":"SKIP: missing database"}`, false},
		{"failed", `{"Action":"fail","Package":"p","Test":"T"}`, false},
		{"incomplete", `{"Action":"pass","Package":"p","Test":"T"}`, false},
		{"valid", "{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"T\"}\n{\"Action\":\"pass\",\"Package\":\"p\"}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.json")
			if err := os.WriteFile(path, []byte(tc.events), 0600); err != nil {
				t.Fatal(err)
			}
			os.Args = []string{"report", path, "p"}
			if err := run(); (err == nil) != tc.ok {
				t.Fatalf("result=%v wantPass=%t", err, tc.ok)
			}
		})
	}
}

func TestGateRequiresDistinctDisposableDatabases(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"report", "validate-databases"}
	for _, key := range []string{"FORUM_STORE_TEST_DSN", "FORUM_API_TEST_DSN", "FORUM_MIGRATION_TEST_DSN"} {
		t.Setenv(key, "")
	}
	if err := run(); err == nil {
		t.Fatal("accepted missing database")
	}
	t.Setenv("FORUM_STORE_TEST_DSN", "dbname=forum")
	if err := run(); err == nil {
		t.Fatal("accepted business database")
	}
	for _, key := range []string{"FORUM_STORE_TEST_DSN", "FORUM_API_TEST_DSN", "FORUM_MIGRATION_TEST_DSN"} {
		t.Setenv(key, "dbname=gobbs_test_same")
	}
	if err := run(); err == nil {
		t.Fatal("accepted shared database")
	}
	t.Setenv("FORUM_API_TEST_DSN", "dbname=gobbs_test_api")
	t.Setenv("FORUM_MIGRATION_TEST_DSN", "dbname=gobbs_test_migrations")
	if err := run(); err != nil {
		t.Fatal(err)
	}
}
