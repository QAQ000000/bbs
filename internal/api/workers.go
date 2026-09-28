package api

import (
	"context"
	"sync"
)

// RunBackgroundWorkers is shared by forumd and the HTTP load fixture. It returns
// only after all workers have stopped; the caller must cancel before closing DB.
func (s *Server) RunBackgroundWorkers(ctx context.Context) {
	var wg sync.WaitGroup
	for _, work := range []func(context.Context){
		func(ctx context.Context) { s.st.RunMemberWork(ctx, s.log) },
		func(ctx context.Context) { s.st.RunForumStats(ctx, s.log) },
		func(ctx context.Context) { s.st.RunSearchIndex(ctx, s.log) },
		s.RunSubscriptions, s.RunEmails, s.runAnalytics, s.runBountyRefunds,
	} {
		wg.Add(1)
		go func() { defer wg.Done(); work(ctx) }()
	}
	wg.Wait()
}
