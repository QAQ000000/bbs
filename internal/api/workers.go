package api

import (
	"context"
	"sync"
	"time"
)

// RunBackgroundWorkers is shared by forumd and the HTTP load fixture. It returns
// only after all workers have stopped; the caller must cancel before closing DB.
func (s *Server) RunBackgroundWorkers(ctx context.Context) {
	var wg sync.WaitGroup
	for _, work := range []func(context.Context){
		func(ctx context.Context) { s.st.RunMemberWork(ctx, s.log) },
		func(ctx context.Context) { s.st.RunForumStats(ctx, s.log) },
		func(ctx context.Context) { s.st.RunSearchIndex(ctx, s.log) },
		s.RunSubscriptions, s.RunEmails, s.runAnalytics,
	} {
		wg.Add(1)
		go func() { defer wg.Done(); work(ctx) }()
	}
	wg.Wait()
}

func (s *Server) runAnalytics(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := s.st.RefreshPointsLeaderboard(job, time.Now().Truncate(time.Hour), 100); err != nil && ctx.Err() == nil {
			s.log.Warn("leaderboard snapshot failed", "err", err)
		}
		if err := s.st.RefreshSiteReport(job, time.Now().Truncate(24*time.Hour)); err != nil && ctx.Err() == nil {
			s.log.Warn("site snapshot failed", "err", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
