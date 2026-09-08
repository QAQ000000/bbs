package store

import (
	"context"
	"errors"
	"testing"
)

func TestMemberWorkerBurstLimits(t *testing.T) {
	ctx := context.Background()
	n, err := processWorkBurst(ctx, func(context.Context) (int, error) { return 50, nil }, func() bool { return false })
	if err != nil || n != 500 {
		t.Fatal("batch cap", n, err)
	}
	calls := 0
	n, err = processWorkBurst(ctx, func(context.Context) (int, error) { calls++; return 50, nil }, func() bool { return calls == 2 })
	if err != nil || n != 100 || calls != 2 {
		t.Fatal("pool pressure ignored", n, calls, err)
	}
	calls = 0
	n, err = processWorkBurst(ctx, func(context.Context) (int, error) { calls++; return 0, nil }, func() bool { return false })
	if err != nil || calls != 1 || n != 0 {
		t.Fatal("empty queue spun", n, calls, err)
	}
	want := errors.New("failed transaction")
	_, err = processWorkBurst(ctx, func(context.Context) (int, error) { return 0, want }, func() bool { return false })
	if !errors.Is(err, want) {
		t.Fatal("failed batch ignored", err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	_, err = processWorkBurst(stopped, func(context.Context) (int, error) { t.Fatal("ran after cancellation"); return 0, nil }, func() bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
