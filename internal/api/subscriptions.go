// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"dzforum/internal/store"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// One durable event and at most 50 recipients per call; membership is reloaded
// for each recipient rather than inheriting the publisher's privileges.
func (s *Server) ProcessSubscriptions(ctx context.Context) (int64, error) {
	var post *store.Post
	var thread *store.Thread
	batch, err := s.st.ProcessSubscriptionBatch(ctx, 50, func(uid, pid int64) (string, bool, bool, error) {
		if post == nil {
			var err error
			post, err = s.st.Post(ctx, pid)
			if errors.Is(err, store.ErrNotFound) {
				return "", false, false, nil
			}
			if err != nil {
				return "", false, false, err
			}
			thread, err = s.st.Thread(ctx, post.ThreadID)
			if errors.Is(err, store.ErrNotFound) {
				return "", false, false, nil
			}
			if err != nil {
				return "", false, false, err
			}
			r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			from := &store.User{ID: post.AuthorID, Username: post.AuthorName}
			mentions := s.notifyMentions(r, from, post.ContentMD, thread, post)
			s.notifyReply(r, from, thread, post, mentions)
		}
		if post == nil || thread == nil || post.Pending || thread.Pending {
			return "", false, false, nil
		}
		target, err := s.st.UserByID(ctx, uid)
		if errors.Is(err, store.ErrNotFound) {
			return "", false, false, nil
		}
		if err != nil {
			return "", false, false, err
		}
		if target.IsBlocked() {
			return "", false, false, nil
		}
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		rr, err := s.loadMembership(requestWithUser(r, target))
		if err != nil {
			return "", false, false, err
		}
		if !s.canViewThread(rr, thread) {
			return "", false, false, nil
		}
		prefs, err := s.st.NotificationPreferences(ctx, uid)
		if err != nil {
			return "", false, false, err
		}
		return "subscription", prefs["subscriptions"], prefs["email"], nil
	})
	if err != nil {
		return 0, err
	}
	if post != nil && thread != nil {
		for _, d := range batch.Deliveries {
			target, err := s.st.UserByID(ctx, d.UID)
			if err != nil || target.IsBlocked() {
				continue
			}
			r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			rr, err := s.loadMembership(requestWithUser(r, target))
			if err != nil || !s.canViewThread(rr, thread) {
				continue
			}
			if d.Email && s.mailer.Enabled() {
				s.mailer.NotifyReply(target.Email, post.AuthorName, thread.Title, s.cfg.SiteURL+ThreadURL(thread.ID, 1)+"#post"+strconv.FormatInt(post.ID, 10), truncate(post.ContentMD, 60))
			}
			if d.InApp {
				s.publish("u:"+idString(d.UID), eventBody{Type: "notify", NotifyCount: int(s.st.UnreadCount(rr.Context(), d.UID))})
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
			_, err := s.ProcessSubscriptions(jobCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.log.Error("subscription delivery failed", "err", err)
			}
		}
	}
}
