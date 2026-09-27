// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"dzforum/internal/store"
)

func (s *Server) ProcessSubscriptions(ctx context.Context) (int64, error) {
	return s.processSubscriptions(ctx, func(ids []int64) error { return s.publishSubscriptionCounts(ctx, ids) })
}

// afterCommit only handles best-effort live counts, never durable delivery.
func (s *Server) processSubscriptions(ctx context.Context, afterCommit func([]int64) error) (int64, error) {
	pid, err := s.st.NextSubscriptionPost(ctx)
	if err != nil || pid == 0 {
		return 0, err
	}
	post, err := s.st.Post(ctx, pid)
	if errors.Is(err, store.ErrNotFound) {
		return pid, nil
	}
	if err != nil {
		return 0, err
	}
	thread, err := s.st.Thread(ctx, post.ThreadID)
	if errors.Is(err, store.ErrNotFound) {
		return pid, nil
	}
	if err != nil {
		return 0, err
	}
	if post.Pending || thread.Pending {
		return pid, nil
	}
	// Preserve mention/direct-reply precedence without holding a subscription transaction.
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	from := &store.User{ID: post.AuthorID, Username: post.AuthorName}
	mentions := s.notifyMentions(r, from, post.ContentMD, thread, post)
	s.notifyReply(r, from, thread, post, mentions)
	batch, err := s.st.ProcessSubscriptionBatch(ctx, 50, pid, s.mailer.Enabled())
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for _, d := range batch.Deliveries {
		if d.InApp {
			ids = append(ids, d.UID)
		}
	}
	return batch.PostID, afterCommit(ids)
}

// Persistent notifications and mail have already committed. Unread counts only
// serve connected SSE clients; offline users read fresh counts through the API.
func (s *Server) publishSubscriptionCounts(ctx context.Context, ids []int64) error {
	online := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, uid := range ids {
		if !seen[uid] && s.hub.HasSubscribers("u:"+idString(uid)) {
			online = append(online, uid)
			seen[uid] = true
		}
	}
	if len(online) == 0 {
		return nil
	}
	counts, err := s.st.NotificationCounts(ctx, online)
	if err != nil {
		return err
	}
	for uid, count := range counts {
		s.publish("u:"+idString(uid), eventBody{Type: "notify", NotifyCount: int(count)})
	}
	return nil
}

func (s *Server) RunSubscriptions(ctx context.Context) {
	runSubscriptionWorker(ctx, s.processSubscriptionWork, s.subscriptionPoolBusy, func(err error) {
		s.log.Error("subscription delivery failed", "err", err)
	})
}

func (s *Server) subscriptionPoolBusy() bool {
	stats := s.st.DatabasePoolStats()
	return stats.Acquired*4 >= stats.Max*3
}

const (
	subscriptionIdleDelay    = time.Second
	subscriptionCatchupDelay = 50 * time.Millisecond
	subscriptionRetryMax     = 30 * time.Second
)

type subscriptionStop uint8

const (
	subscriptionEmpty subscriptionStop = iota
	subscriptionBudget
	subscriptionBusy
	subscriptionFailed
	subscriptionCanceled
)

// Batches are committed processing attempts, not completed events or recipients.
// A single post may need several recipient batches.
type subscriptionWorkResult struct {
	batches int
	stop    subscriptionStop
}

type subscriptionSchedule struct {
	retry time.Duration
}

func (s *subscriptionSchedule) next(result subscriptionWorkResult, err error, busy bool) time.Duration {
	if err != nil {
		if s.retry == 0 {
			s.retry = subscriptionIdleDelay
		} else {
			s.retry = min(s.retry*2, subscriptionRetryMax)
		}
		return s.retry
	}
	s.retry = 0
	if result.stop == subscriptionBudget && !busy {
		return subscriptionCatchupDelay
	}
	return subscriptionIdleDelay
}

// One consumer, including during catch-up. Recheck pressure after waiting so
// a queued fast round cannot bypass newly arrived foreground work. Regular
// rounds still get one bounded batch under pressure, preventing total starvation.
func runSubscriptionWorker(ctx context.Context, run func(context.Context) (subscriptionWorkResult, error), busy func() bool, report func(error)) {
	timer := time.NewTimer(subscriptionIdleDelay)
	defer timer.Stop()
	var schedule subscriptionSchedule
	catchup := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		if catchup && busy() {
			catchup = false
			timer.Reset(subscriptionIdleDelay)
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := run(jobCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			report(err)
		}
		delay := schedule.next(result, err, busy())
		catchup = delay == subscriptionCatchupDelay
		timer.Reset(delay)
	}
}

// Budgets and pressure are checked only between complete transactions. Always
// permit the first batch of a regular round; pause additional work at 75% pool
// occupancy. The caller's deadline bounds stuck queries and shutdown.
func processSubscriptionBurst(ctx context.Context, run func(context.Context) (int64, error), busy func() bool) (subscriptionWorkResult, error) {
	start := time.Now()
	result := subscriptionWorkResult{}
	for {
		if err := ctx.Err(); err != nil {
			result.stop = subscriptionCanceled
			return result, err
		}
		if result.batches >= 100 || time.Since(start) >= 250*time.Millisecond {
			result.stop = subscriptionBudget
			return result, nil
		}
		if result.batches > 0 && busy() {
			result.stop = subscriptionBusy
			return result, nil
		}
		pid, err := run(ctx)
		if err != nil {
			result.stop = subscriptionFailed
			return result, err
		}
		if pid == 0 {
			result.stop = subscriptionEmpty
			return result, nil
		}
		result.batches++
	}
}

// Coalesce overlapping recipients across committed batches. Even if a later
// delivery fails, publish counts for earlier commits; durable cursors retain
// failed work. Count queries use groups of at most 50 under the same deadline.
func (s *Server) processSubscriptionWork(ctx context.Context) (subscriptionWorkResult, error) {
	var ids []int64
	seen := make(map[int64]bool)
	n, err := processSubscriptionBurst(ctx, func(ctx context.Context) (int64, error) {
		return s.processSubscriptions(ctx, func(batch []int64) error {
			for _, id := range batch {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			return nil
		})
	}, s.subscriptionPoolBusy)
	for len(ids) > 0 {
		end := min(50, len(ids))
		if publishErr := s.publishSubscriptionCounts(ctx, ids[:end]); publishErr != nil {
			n.stop = subscriptionFailed
			return n, errors.Join(err, publishErr)
		}
		ids = ids[end:]
	}
	return n, err
}
