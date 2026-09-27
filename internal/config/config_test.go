package config

import "testing"

func TestDatabaseConfigurationHasNoImplicitCredentials(t *testing.T) {
	t.Setenv("FORUM_DSN", "")
	if got := FromEnv().DSN; got != "" {
		t.Fatal("missing database configuration acquired a fallback")
	}
	t.Setenv("FORUM_DSN", "host=/tmp/forum-test user=app dbname=forum")
	if got := FromEnv().DSN; got != "host=/tmp/forum-test user=app dbname=forum" {
		t.Fatal("explicit database configuration was changed")
	}
}
