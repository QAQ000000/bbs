package db

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("FORUM_REQUIRE_TEST_DB") == "1" && os.Getenv("FORUM_MIGRATION_TEST_DSN") == "" {
		fmt.Fprintln(os.Stderr, "required migration test database is missing")
		os.Exit(1)
	}
	os.Exit(m.Run())
}
