package store

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RunMemberWork keeps the existing ordered transactions and accelerates backlog
// draining with short bursts. It joins both workers before returning.
func (s *Store) RunMemberWork(ctx context.Context, logger *slog.Logger) {
	var wg sync.WaitGroup
	for _, task := range []struct {
		name  string
		limit int
		run   func(context.Context, int) (int, error)
	}{{"growth", 100, s.ProcessMemberEvents}, {"titles", 50, s.ProcessTitleWork}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					job, cancel := context.WithTimeout(ctx, 10*time.Second)
					_, err := processWorkBurst(job, func(ctx context.Context) (int, error) {
						return task.run(ctx, task.limit)
					}, func() bool {
						stats := s.pool.Stat()
						return stats.AcquiredConns()*4 >= stats.MaxConns()*3
					})
					cancel()
					if err != nil && ctx.Err() == nil {
						logger.Error("member worker failed", "queue", task.name, "err", err)
					}
				}
			}
		}()
	}
	wg.Wait()
}

// The time budget is checked between transactions, never used to split one.
func processWorkBurst(ctx context.Context, run func(context.Context) (int, error), busy func() bool) (int, error) {
	start := time.Now()
	total := 0
	for batch := 0; batch < 10 && time.Since(start) < 250*time.Millisecond; batch++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		if busy() {
			break
		}
		n, err := run(ctx)
		total += n
		if err != nil || n == 0 {
			return total, err
		}
	}
	return total, nil
}
