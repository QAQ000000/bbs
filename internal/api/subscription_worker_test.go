package api

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestSubscriptionWorkerCatchupPressureAndIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var pending, calls atomic.Int64
		pending.Store(250)
		var busy atomic.Bool
		var resultsMu sync.Mutex
		var results []subscriptionWorkResult
		go runSubscriptionWorker(ctx, func(ctx context.Context) (subscriptionWorkResult, error) {
			calls.Add(1)
			r, err := processSubscriptionBurst(ctx, func(context.Context) (int64, error) {
				if pending.Load() == 0 {
					return 0, nil
				}
				pending.Add(-1)
				return 1, nil
			}, func() bool { return busy.Load() })
			resultsMu.Lock()
			results = append(results, r)
			resultsMu.Unlock()
			return r, err
		}, func() bool { return busy.Load() }, func(err error) { t.Error(err) })
		step := func(d time.Duration, wantCalls, wantPending int) {
			t.Helper()
			time.Sleep(d)
			synctest.Wait()
			if calls.Load() != int64(wantCalls) || pending.Load() != int64(wantPending) {
				t.Fatalf("calls=%d pending=%d want=%d/%d", calls.Load(), pending.Load(), wantCalls, wantPending)
			}
		}
		step(time.Second, 1, 150)
		// Pressure can rise after a fast round was scheduled. That round must yield.
		busy.Store(true)
		step(50*time.Millisecond, 1, 150)
		// Even continuously busy pools allow a regular round to make one batch.
		step(time.Second, 2, 149)
		busy.Store(false)
		step(time.Second, 3, 49)
		step(50*time.Millisecond, 4, 0)
		// Empty work must return to idle polling, not spin on the fast timer.
		step(999*time.Millisecond, 4, 0)
		step(time.Millisecond, 5, 0)
		want := []subscriptionWorkResult{{100, subscriptionBudget}, {1, subscriptionBusy}, {100, subscriptionBudget}, {49, subscriptionEmpty}, {0, subscriptionEmpty}}
		resultsMu.Lock()
		if !reflect.DeepEqual(results, want) {
			t.Errorf("rounds=%v want=%v", results, want)
		}
		resultsMu.Unlock()
		cancel()
		synctest.Wait()
	})
}

func TestSubscriptionWorkerFailureBackoffAndCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		failure := errors.New("database unavailable")
		started := time.Now()
		var attempts []time.Duration
		reports := 0
		go runSubscriptionWorker(ctx, func(ctx context.Context) (subscriptionWorkResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Sub(time.Now()) != 10*time.Second {
				t.Error("missing bounded job context")
			}
			attempts = append(attempts, time.Since(started))
			return subscriptionWorkResult{stop: subscriptionFailed}, failure
		}, func() bool { return false }, func(err error) {
			if !errors.Is(err, failure) {
				t.Error(err)
			}
			reports++
		})
		time.Sleep(100 * time.Second)
		synctest.Wait()
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 62 * time.Second, 92 * time.Second}
		if !reflect.DeepEqual(attempts, want) || reports != len(want) {
			t.Fatalf("attempts=%v reports=%d", attempts, reports)
		}
		// Cancellation must interrupt the 30s backoff immediately.
		before := time.Now()
		cancel()
		synctest.Wait()
		if !time.Now().Equal(before) {
			t.Fatal("shutdown waited for retry timer")
		}
	})
}

func TestSubscriptionWorkerCancelsRunningBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entered, finished := false, false
		go runSubscriptionWorker(ctx, func(ctx context.Context) (subscriptionWorkResult, error) {
			entered = true
			<-ctx.Done()
			finished = true
			return subscriptionWorkResult{stop: subscriptionCanceled}, ctx.Err()
		}, func() bool { return false }, func(err error) { t.Errorf("shutdown logged as failure: %v", err) })
		time.Sleep(time.Second)
		synctest.Wait()
		if !entered || finished {
			t.Fatal("batch not in flight")
		}
		cancel()
		synctest.Wait()
		if !finished {
			t.Fatal("batch survived cancellation")
		}
	})
}

func TestSubscriptionScheduleResetsAfterRecovery(t *testing.T) {
	var s subscriptionSchedule
	failure := errors.New("temporary failure")
	for range 7 {
		s.next(subscriptionWorkResult{stop: subscriptionFailed}, failure, false)
	}
	if d := s.next(subscriptionWorkResult{batches: 100, stop: subscriptionBudget}, nil, false); d != subscriptionCatchupDelay {
		t.Fatal("recovered backlog did not resume", d)
	}
	if d := s.next(subscriptionWorkResult{}, failure, false); d != time.Second {
		t.Fatal("retry delay not reset", d)
	}
	for _, reason := range []subscriptionStop{subscriptionEmpty, subscriptionBusy} {
		if d := s.next(subscriptionWorkResult{stop: reason}, nil, false); d != time.Second {
			t.Fatal(reason, d)
		}
	}
	if d := s.next(subscriptionWorkResult{stop: subscriptionBudget}, nil, true); d != time.Second {
		t.Fatal("pressure bypassed", d)
	}
}

func TestSubscriptionBurstStopReasons(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		idle := func() bool { return false }
		failure := errors.New("batch failed")
		n, err := processSubscriptionBurst(ctx, func(context.Context) (int64, error) { return 0, nil }, idle)
		if err != nil || n.stop != subscriptionEmpty {
			t.Fatal(n, err)
		}
		n, err = processSubscriptionBurst(ctx, func(context.Context) (int64, error) { return 0, failure }, idle)
		if !errors.Is(err, failure) || n.stop != subscriptionFailed {
			t.Fatal(n, err)
		}
		child, cancel := context.WithCancel(ctx)
		n, err = processSubscriptionBurst(child, func(context.Context) (int64, error) { cancel(); return 1, nil }, idle)
		if !errors.Is(err, context.Canceled) || n.stop != subscriptionCanceled || n.batches != 1 {
			t.Fatal(n, err)
		}
		// A transaction can exceed the soft budget; finish it and yield afterwards.
		n, err = processSubscriptionBurst(ctx, func(context.Context) (int64, error) { time.Sleep(260 * time.Millisecond); return 1, nil }, idle)
		if err != nil || n.stop != subscriptionBudget || n.batches != 1 {
			t.Fatal(n, err)
		}
	})
}
