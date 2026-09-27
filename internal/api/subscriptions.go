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
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := s.processSubscriptionWork(jobCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.log.Error("subscription delivery failed", "err", err)
			}
		}
	}
}

// A backlog can consume more than 20 cheap batches per tick, but yields after
// 250ms or 100 batches. Check the soft budget between complete transactions;
// the caller's context still bounds a stuck query and supports shutdown.
func processSubscriptionBurst(ctx context.Context, run func(context.Context) (int64, error)) (int, error) {
	start := time.Now()
	processed := 0
	for processed < 100 && time.Since(start) < 250*time.Millisecond {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		pid, err := run(ctx)
		if err != nil || pid == 0 {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// Coalesce overlapping recipients across committed batches. Even if a later
// delivery fails, publish counts for earlier commits; durable cursors retain
// failed work. Count queries use groups of at most 50 under the same deadline.
func (s *Server) processSubscriptionWork(ctx context.Context) (int, error) {
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
	})
	for len(ids) > 0 {
		end := min(50, len(ids))
		if publishErr := s.publishSubscriptionCounts(ctx, ids[:end]); publishErr != nil {
			return n, errors.Join(err, publishErr)
		}
		ids = ids[end:]
	}
	return n, err
}
