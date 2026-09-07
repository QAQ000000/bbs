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
	counts, err := s.st.NotificationCounts(ctx, ids)
	if err != nil {
		return batch.PostID, err
	}
	for _, d := range batch.Deliveries {
		if d.InApp {
			if count, ok := counts[d.UID]; ok {
				s.publish("u:"+idString(d.UID), eventBody{Type: "notify", NotifyCount: int(count)})
			}
		}
	}
	return batch.PostID, nil
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
			var err error
			for batch := 0; batch < 20 && jobCtx.Err() == nil; batch++ {
				var pid int64
				pid, err = s.ProcessSubscriptions(jobCtx)
				if err != nil || pid == 0 {
					break
				}
			}
			cancel()
			if err != nil && ctx.Err() == nil {
				s.log.Error("subscription delivery failed", "err", err)
			}
		}
	}
}
