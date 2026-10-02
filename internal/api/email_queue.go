package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"dzforum/internal/mail"
	"dzforum/internal/store"
)

var errEmailCancelled = errors.New("email no longer eligible")

func (s *Server) prepareEmail(ctx context.Context, j *store.EmailJob) (mail.Message, error) {
	msg := mail.Message{ID: j.ID, To: j.Recipient, Kind: j.Kind}
	settings, err := s.st.Settings(ctx)
	if err != nil {
		return msg, err
	}
	msg.SiteName = settings.SiteName
	u, err := s.st.UserByID(ctx, j.UID)
	if errors.Is(err, store.ErrNotFound) {
		return msg, errEmailCancelled
	}
	if err != nil {
		return msg, err
	}
	if !time.Now().Before(j.ExpiresAt) {
		return msg, errEmailCancelled
	}
	// A security notice is deliberately addressed to the previous recovery address.
	if j.Kind == "email_changed" {
		return msg, nil
	}
	if u.IsBlocked() || (j.Kind != "email_change" && (u.Email == "" || !strings.EqualFold(u.Email, j.Recipient))) {
		return msg, errEmailCancelled
	}
	if j.Kind == "password_reset" || j.Kind == "email_verify" || j.Kind == "email_change" {
		valid, err := s.st.AuthEmailValid(ctx, j)
		if err != nil {
			return msg, err
		}
		if !valid {
			return msg, errEmailCancelled
		}
		raw, err := s.mailTokens.Open(j.SealedToken)
		if err != nil {
			return msg, err
		}
		path := "/reset?token="
		if j.Kind == "email_verify" {
			path = "/verify?token="
		}
		if j.Kind == "email_change" {
			path = "/settings/email/confirm?token="
		}
		msg.Link = strings.TrimRight(s.cfg.SiteURL, "/") + path + url.QueryEscape(raw)
		return msg, nil
	}
	prefs, err := s.st.NotificationPreferences(ctx, j.UID)
	if err != nil {
		return msg, err
	}
	group := "replies"
	if j.Kind == "mention" {
		group = "mentions"
	}
	if j.Kind == "subscription" {
		group = "subscriptions"
	}
	if !prefs["email"] || !prefs[group] {
		return msg, errEmailCancelled
	}
	p, err := s.st.Post(ctx, j.PostID)
	if errors.Is(err, store.ErrNotFound) {
		return msg, errEmailCancelled
	}
	if err != nil {
		return msg, err
	}
	th, err := s.st.Thread(ctx, p.ThreadID)
	if errors.Is(err, store.ErrNotFound) {
		return msg, errEmailCancelled
	}
	if err != nil {
		return msg, err
	}
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	rr, err := s.loadMembership(requestWithUser(r, u))
	if err != nil {
		return msg, err
	}
	if p.Pending || th.Pending || !s.canViewThread(rr, th) {
		return msg, errEmailCancelled
	}
	if j.Kind == "subscription" {
		valid, err := s.st.SubscriptionEmailValid(ctx, j.UID, j.PostID)
		if err != nil {
			return msg, err
		}
		if !valid {
			return msg, errEmailCancelled
		}
	}
	msg.FromName = p.AuthorName
	msg.Title = th.Title
	msg.Excerpt = truncate(p.ContentMD, 60)
	msg.Link = strings.TrimRight(s.cfg.SiteURL, "/") + ThreadURL(th.ID, 1) + "#post" + strconv.FormatInt(p.ID, 10)
	return msg, nil
}

// ProcessEmail handles one persisted task. SMTP acceptance and database commit cannot be atomic.
func (s *Server) ProcessEmail(ctx context.Context) (int64, error) {
	if !s.mailer.Enabled() {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	j, err := s.st.ClaimEmail(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	msg, err := s.prepareEmail(ctx, j)
	status, code := "sent", ""
	if errors.Is(err, errEmailCancelled) {
		status, code = "cancelled", "ineligible"
	} else if err != nil {
		status, code = "pending", "preparation_failed"
	} else if err = s.mailer.Send(ctx, msg); err != nil {
		var permanent bool
		code, permanent = mail.ErrorCode(err)
		status = "pending"
		if permanent {
			status = "dead"
		}
	}
	// Finish even after shutdown cancelled transport; failure here is recovered by the lease.
	done, cancelDone := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelDone()
	if err = s.st.FinishEmail(done, j, status, code); err != nil {
		return j.ID, err
	}
	if status != "sent" && status != "cancelled" {
		s.log.Warn("email delivery deferred", "job", j.ID, "code", code)
	}
	return j.ID, nil
}

func (s *Server) RunEmails(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	if s.mailer.Enabled() {
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				for {
					if ctx.Err() != nil {
						return
					}
					id, err := s.ProcessEmail(ctx)
					if err != nil && ctx.Err() == nil {
						s.log.Error("email worker database failure")
					}
					if id != 0 && err == nil {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case <-tick.C:
					}
				}
			}()
		}
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		maintenance, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.st.MaintainEmails(maintenance)
		cancel()
		if err != nil && ctx.Err() == nil {
			s.log.Error("email queue maintenance failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) adminEmailQueue(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", "pending", "sending", "sent", "dead", "cancelled":
	default:
		s.fail(w, r, 422, "VALIDATION_FAILED", "未知邮件状态")
		return
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			s.fail(w, r, 422, "VALIDATION_FAILED", "无效分页游标")
			return
		}
	}
	jobs, counts, err := s.st.EmailQueuePage(r.Context(), status, before)
	if err != nil {
		s.fail(w, r, 503, "EMAIL_QUEUE_FAILED", "暂时无法读取邮件队列")
		return
	}
	var next string
	if len(jobs) > 50 {
		jobs = jobs[:50]
		next = idString(jobs[len(jobs)-1].ID)
	}
	s.respond(w, 200, map[string]any{"items": jobs, "counts": counts, "nextBefore": next, "smtpEnabled": s.mailer.Enabled()})
}

func (s *Server) adminEmailRetry(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "FORBIDDEN", "表单已过期，请刷新重试")
		return
	}
	id := pathID(r, "jobId")
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if id < 1 || err != nil || version < 1 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "需要有效任务编号和版本号")
		return
	}
	if !s.mailer.Enabled() {
		s.fail(w, r, 409, "SMTP_DISABLED", "尚未配置邮件服务")
		return
	}
	err = s.st.RetryEmail(r.Context(), id, version, User(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, 409, "EMAIL_RETRY_CONFLICT", "任务状态已变更、已过期或不可重试")
		return
	}
	if err != nil {
		s.fail(w, r, 503, "EMAIL_QUEUE_FAILED", "任务重试失败")
		return
	}
	s.respond(w, 200, map[string]any{"message": "邮件任务已重新加入队列"})
}

func (s *Server) adminEmailDetail(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "jobId")
	if id < 1 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "需要有效任务编号")
		return
	}
	job, err := s.st.EmailJob(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, 404, "NOT_FOUND", "邮件任务不存在或已清理")
		return
	}
	if err != nil {
		s.fail(w, r, 503, "EMAIL_QUEUE_FAILED", "暂时无法读取邮件任务")
		return
	}
	s.respond(w, 200, job)
}

func (s *Server) adminEmailCancel(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "FORBIDDEN", "表单已过期，请刷新重试")
		return
	}
	id := pathID(r, "jobId")
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if id < 1 || err != nil || version < 1 {
		s.fail(w, r, 422, "VALIDATION_FAILED", "需要有效任务编号和版本号")
		return
	}
	err = s.st.CancelEmail(r.Context(), id, version, User(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, 409, "EMAIL_CANCEL_CONFLICT", "任务状态已变化；只有待发送或失败任务可取消")
		return
	}
	if err != nil {
		s.fail(w, r, 503, "EMAIL_QUEUE_FAILED", "取消邮件任务失败")
		return
	}
	s.respond(w, 200, map[string]any{"message": "邮件任务已取消"})
}
