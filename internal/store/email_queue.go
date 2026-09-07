package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// EmailJob exposes operational metadata only; addresses and secrets never reach admin JSON.
type EmailJob struct {
	ID            int64      `json:"id,string"`
	UID           int64      `json:"userId,string"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	Retries       int        `json:"retries"`
	Version       int64      `json:"version"`
	NextAttemptAt time.Time  `json:"nextAttemptAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	SentAt        *time.Time `json:"sentAt"`
	LastError     string     `json:"lastError"`
	Recipient     string     `json:"-"`
	PostID        int64      `json:"-"`
	TokenHash     string     `json:"-"`
	SealedToken   string     `json:"-"`
}

const emailCols = `id,uid,kind,status,attempts,retries,version,next_attempt_at,expires_at,created_at,sent_at,last_error,recipient,coalesce(post_id,0),token_hash,sealed_token`

func scanEmail(row pgx.Row) (*EmailJob, error) {
	j := &EmailJob{}
	err := row.Scan(&j.ID, &j.UID, &j.Kind, &j.Status, &j.Attempts, &j.Retries, &j.Version, &j.NextAttemptAt, &j.ExpiresAt, &j.CreatedAt, &j.SentAt, &j.LastError, &j.Recipient, &j.PostID, &j.TokenHash, &j.SealedToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

// QueueAuthEmail commits the token and its encrypted delivery task together.
func (s *Store) QueueAuthEmail(ctx context.Context, uid int64, email, kind string, seal func(string) (string, error)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = queueAuthEmail(ctx, tx, uid, email, kind, seal); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func queueAuthEmail(ctx context.Context, tx pgx.Tx, uid int64, email, kind string, seal func(string) (string, error)) error {
	if kind != "password_reset" && kind != "email_verify" {
		return errors.New("unsupported auth email")
	}
	raw := newToken(32)
	sealed, err := seal(raw)
	if err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&current); err != nil {
		return err
	}
	if current == "" || !strings.EqualFold(current, email) {
		return ErrNotFound
	}
	var expires time.Time
	if kind == "password_reset" {
		err = tx.QueryRow(ctx, `INSERT INTO password_resets(uid,token_hash,expires_at) VALUES($1,$2,now()+interval '15 minutes') RETURNING expires_at`, uid, hashToken(raw)).Scan(&expires)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO email_verifications(uid,token_hash,email,expires_at) VALUES($1,$2,lower($3),now()+interval '24 hours')
 ON CONFLICT(uid) DO UPDATE SET token_hash=EXCLUDED.token_hash,email=EXCLUDED.email,expires_at=EXCLUDED.expires_at,created_at=now() RETURNING expires_at`, uid, hashToken(raw), email).Scan(&expires)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_jobs(uid,kind,recipient,token_hash,sealed_token,dedup_key,priority,expires_at) VALUES($1,$2,$3,$4,$5,$6,10,$7)`, uid, kind, current, hashToken(raw), sealed, kind+":"+hashToken(raw), expires)
	if err != nil {
		return err
	}
	return nil
}

func queuePostEmail(ctx context.Context, tx pgx.Tx, uid, pid int64, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO email_jobs(uid,kind,recipient,post_id,dedup_key,expires_at)
 SELECT u.id,$3,u.email,$2,'post:'||$2::bigint::text||':user:'||u.id,now()+interval '7 days'
 FROM users u LEFT JOIN notification_preferences np ON np.uid=u.id
 WHERE u.id=$1 AND u.email<>'' AND coalesce((np.body->>'email')::boolean,true)
 ON CONFLICT(dedup_key) DO NOTHING`, uid, pid, kind)
	return err
}

func (s *Store) ClaimEmail(ctx context.Context) (*EmailJob, error) {
	// A lease is longer than the worker's complete preparation + SMTP deadline.
	return scanEmail(s.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM email_jobs WHERE expires_at>now() AND attempts<8 AND
 ((status='pending' AND next_attempt_at<=now()) OR (status='sending' AND lease_until<now()))
 ORDER BY priority DESC,next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE email_jobs j SET status='sending',attempts=attempts+1,version=version+1,lease_until=now()+interval '2 minutes',updated_at=now()
 FROM candidate c WHERE j.id=c.id RETURNING `+emailQualifiedCols()))
}

func emailQualifiedCols() string {
	// RETURNING joins another id; qualify every selected job column.
	return `j.id,j.uid,j.kind,j.status,j.attempts,j.retries,j.version,j.next_attempt_at,j.expires_at,j.created_at,j.sent_at,j.last_error,j.recipient,coalesce(j.post_id,0),j.token_hash,j.sealed_token`
}

func (s *Store) AuthEmailValid(ctx context.Context, j *EmailJob) (bool, error) {
	var valid bool
	query := `SELECT EXISTS(SELECT 1 FROM password_resets WHERE uid=$1 AND token_hash=$2 AND NOT used AND expires_at>now())`
	if j.Kind == "email_verify" {
		query = `SELECT EXISTS(SELECT 1 FROM email_verifications WHERE uid=$1 AND token_hash=$2 AND expires_at>now() AND email=lower($3))`
		err := s.pool.QueryRow(ctx, query, j.UID, j.TokenHash, j.Recipient).Scan(&valid)
		return valid, err
	}
	err := s.pool.QueryRow(ctx, query, j.UID, j.TokenHash).Scan(&valid)
	return valid, err
}

// FinishEmail uses a fencing version, so a late worker cannot overwrite a new lease.
func (s *Store) FinishEmail(ctx context.Context, j *EmailJob, status, code string) error {
	if status != "sent" && status != "pending" && status != "dead" && status != "cancelled" {
		return errors.New("invalid email result")
	}
	if status == "pending" && j.Attempts >= 8 {
		status = "dead"
	}
	delay := 30 * time.Second * time.Duration(1<<min(j.Attempts-1, 7))
	tag, err := s.pool.Exec(ctx, `UPDATE email_jobs SET status=$3,last_error=$4,lease_until=NULL,
 next_attempt_at=now()+$5::interval,sent_at=CASE WHEN $3='sent' THEN now() ELSE sent_at END,
 sealed_token=CASE WHEN $3 IN ('sent','cancelled') THEN '' ELSE sealed_token END,updated_at=now()
 WHERE id=$1 AND version=$2 AND status='sending'`, j.ID, j.Version, status, code, delay.String())
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) MaintainEmails(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE email_jobs SET status=CASE WHEN expires_at<=now() THEN 'cancelled' ELSE 'dead' END,
 sealed_token=CASE WHEN expires_at<=now() THEN '' ELSE sealed_token END,
 last_error=CASE WHEN expires_at<=now() THEN 'expired' ELSE 'lease_exhausted' END,lease_until=NULL,updated_at=now()
 WHERE (status IN ('pending','dead') OR (status='sending' AND lease_until<now()))
 AND (expires_at<=now() OR (status='sending' AND attempts>=8))`)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM email_jobs WHERE status IN ('sent','dead','cancelled') AND expires_at<now()-interval '30 days'`)
	return err
}

func (s *Store) EmailQueuePage(ctx context.Context, status string, before int64) ([]*EmailJob, map[string]int64, error) {
	counts := map[string]int64{"pending": 0, "sending": 0, "sent": 0, "dead": 0, "cancelled": 0}
	rows, err := s.pool.Query(ctx, `SELECT status,count(*) FROM email_jobs GROUP BY status`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var k string
		var n int64
		if err = rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, nil, err
		}
		counts[k] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT `+emailCols+` FROM email_jobs WHERE ($1='' OR status=$1) AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 51`, status, before)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []*EmailJob{}
	for rows.Next() {
		j, e := scanEmail(rows)
		if e != nil {
			return nil, nil, e
		}
		out = append(out, j)
	}
	return out, counts, rows.Err()
}

func (s *Store) RetryEmail(ctx context.Context, id, version, actor int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE email_jobs SET status='pending',attempts=0,retries=retries+1,version=version+1,next_attempt_at=now(),last_error='',updated_at=now()
 WHERE id=$1 AND version=$2 AND status='dead' AND expires_at>now()`, id, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	// The audit row must commit with the retry.
	_, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail) SELECT id,username,'email.retry',$2 FROM users WHERE id=$1`, actor, fmt.Sprintf("email_job=%d", id))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SubscriptionEmailValid(ctx context.Context, uid, pid int64) (bool, error) {
	var valid bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM posts p JOIN threads t ON t.id=p.thread_id
 JOIN subscription_events e ON e.post_id=p.id WHERE p.id=$2 AND (
 EXISTS(SELECT 1 FROM thread_subscriptions s WHERE s.uid=$1 AND s.thread_id=t.id AND s.enabled AND s.notify_email AND s.created_at<=e.created_at AND (s.muted_until IS NULL OR s.muted_until<=now()))
 OR (p.floor=1 AND EXISTS(SELECT 1 FROM forum_subscriptions s WHERE s.uid=$1 AND s.forum_id=t.forum_id AND s.enabled AND s.notify_email AND s.created_at<=e.created_at AND (s.muted_until IS NULL OR s.muted_until<=now())))
 OR (p.floor=1 AND EXISTS(SELECT 1 FROM tag_subscriptions s JOIN tags g ON g.id=s.tag_id AND g.status='active' JOIN thread_tags tt ON tt.tag_id=s.tag_id AND tt.thread_id=t.id WHERE s.uid=$1 AND s.enabled AND s.notify_email AND s.created_at<=e.created_at AND tt.created_at<=e.created_at AND (s.muted_until IS NULL OR s.muted_until<=now())))
 ))`, uid, pid).Scan(&valid)
	return valid, err
}
