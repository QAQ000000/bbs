package api

import (
	"context"
	"time"
)

const (
	analyticsRefreshInterval   = time.Hour
	analyticsJobTimeout        = 30 * time.Second
	analyticsRetryInitial      = 5 * time.Second
	analyticsRetryMax          = 5 * time.Minute
	analyticsStaleAfter        = analyticsRefreshInterval + analyticsRetryMax
	analyticsRetentionInterval = time.Minute
)

type analyticsJob struct {
	name     string
	refresh  func(context.Context) error
	next     time.Time
	retry    time.Duration
	interval time.Duration
}

func (s *Server) runAnalytics(ctx context.Context) {
	runAnalyticsWorker(ctx, []analyticsJob{
		{name: "points", refresh: func(ctx context.Context) error {
			return s.st.RefreshPointsLeaderboard(ctx, time.Now().UTC().Truncate(time.Hour), 100)
		}},
		{name: "points.day", refresh: func(ctx context.Context) error {
			return s.st.RefreshPeriodLeaderboard(ctx, "day", time.Now())
		}},
		{name: "points.week", refresh: func(ctx context.Context) error {
			return s.st.RefreshPeriodLeaderboard(ctx, "week", time.Now())
		}},
		{name: "points.month", refresh: func(ctx context.Context) error {
			return s.st.RefreshPeriodLeaderboard(ctx, "month", time.Now())
		}},
		{name: "site", refresh: func(ctx context.Context) error {
			return s.st.RefreshSiteReport(ctx, time.Now().UTC().Truncate(24*time.Hour))
		}},
		{name: "retention", interval: analyticsRetentionInterval, refresh: s.pruneAnalyticsSnapshots},
	}, func(name string, err error, retry time.Duration) {
		s.log.Warn("analytics snapshot failed", "name", name, "err", err, "retryIn", retry)
	})
}

func (s *Server) pruneAnalyticsSnapshots(ctx context.Context) error {
	deleted, err := s.st.PruneAnalyticsSnapshots(ctx)
	if err == nil && deleted > 0 {
		s.log.Info("analytics snapshots pruned", "deleted", deleted)
	}
	return err
}

// Keep refreshes sequential, but give each snapshot its own deadline and retry
// schedule. A failed leaderboard must not consume the report's timeout or make
// either snapshot wait an entire hour for recovery. Restart refreshes both.
func runAnalyticsWorker(ctx context.Context, jobs []analyticsJob, report func(string, error, time.Duration)) {
	if len(jobs) == 0 {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for i := range jobs {
			if ctx.Err() != nil {
				return
			}
			j := &jobs[i]
			if time.Now().Before(j.next) {
				continue
			}
			jobCtx, cancel := context.WithTimeout(ctx, analyticsJobTimeout)
			err := j.refresh(jobCtx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			delay := j.interval
			if delay <= 0 {
				delay = analyticsRefreshInterval
			}
			if err != nil {
				if j.retry == 0 {
					j.retry = analyticsRetryInitial
				} else {
					j.retry = min(j.retry*2, analyticsRetryMax)
				}
				delay = j.retry
				report(j.name, err, delay)
			} else {
				j.retry = 0
			}
			j.next = time.Now().Add(delay)
		}
		next := jobs[0].next
		for _, j := range jobs[1:] {
			if j.next.Before(next) {
				next = j.next
			}
		}
		timer.Reset(max(time.Until(next), 0))
	}
}
