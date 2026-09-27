package main

import "testing"

func TestRejectNonDisposableDSN(t *testing.T) {
	for _, dsn := range []string{
		"", "postgres://prod@127.0.0.1/forum", "host=/tmp dbname=gobbs_test_worker user=gobbs_test_worker sslmode=disable",
		"host=/tmp/gobbs-recovery-cluster.test dbname=forum user=gobbs_test_worker sslmode=disable",
		"host=/tmp/gobbs-recovery-cluster.test dbname=gobbs_test_worker user=postgres sslmode=disable",
		"host=127.0.0.1 dbname=gobbs_test_worker user=gobbs_test_worker sslmode=disable",
		"host=/tmp/gobbs-recovery-cluster.test,/tmp dbname=gobbs_test_worker user=gobbs_test_worker sslmode=disable",
	} {
		if err := validateDSN(dsn); err == nil {
			t.Fatalf("accepted non-private DSN %q", dsn)
		}
	}
	if err := validateDSN("host=/tmp/gobbs-recovery-cluster.test dbname=gobbs_test_worker user=gobbs_test_worker sslmode=disable"); err != nil {
		t.Fatal(err)
	}
}
