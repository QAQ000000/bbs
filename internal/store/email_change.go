package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"dzforum/internal/mfa"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

var ErrEmailUnavailable = errors.New("email unavailable")

// RequestEmailChange leaves the recovery address intact until confirmation.
func (s *Store) RequestEmailChange(ctx context.Context, uid, sessionID int64, email, password, code, recovery, ip string, cipher *mfa.Cipher, seal func(string) (string, error)) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ErrEmailUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	username, hash, err := lockMFAUser(ctx, tx, uid)
	if err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>now())`, sessionID, uid).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrMFAInvalid
	}
	if err = mfaAttempt(ctx, tx, uid, "email.change", 5, "1 hour"); err != nil {
		if errors.Is(err, ErrMFALimited) {
			return mfaReject(ctx, tx, uid, username, ip, err)
		}
		return err
	}
	reject := func() error { return mfaReject(ctx, tx, uid, username, ip, ErrMFAInvalid) }
	if len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return reject()
	}
	var v MFAState
	err = tx.QueryRow(ctx, `SELECT enabled,secret_cipher,last_step,recovery_hashes,version FROM user_mfa WHERE user_id=$1 AND enabled FOR UPDATE`, uid).Scan(&v.Enabled, &v.SecretCipher, &v.LastStep, &v.RecoveryHashes, &v.Version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if v.Enabled {
		secret, e := cipher.Open(v.SecretCipher)
		if e != nil {
			return ErrMFAUnavailable
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		step, hashes, used, ok := mfaProof(v, secret, code, recovery, now)
		if !ok {
			return reject()
		}
		if _, err = tx.Exec(ctx, `UPDATE user_mfa SET last_step=$2,recovery_hashes=$3,updated_at=now() WHERE user_id=$1`, uid, step, hashes); err != nil {
			return err
		}
		if used {
			if err = mfaAudit(ctx, tx, uid, username, "recovery.used", ip); err != nil {
				return err
			}
		}
	}
	var old string
	var taken bool
	if err = tx.QueryRow(ctx, `SELECT email,EXISTS(SELECT 1 FROM users WHERE lower(email)=$2 AND id<>$1) FROM users WHERE id=$1`, uid, email).Scan(&old, &taken); err != nil {
		return err
	}
	if strings.EqualFold(old, email) || taken {
		return ErrEmailUnavailable
	}
	raw := newToken(32)
	sealed, err := seal(raw)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_changes(uid,new_email,old_email,password_hash,session_id,mfa_version,token_hash,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,now()+interval '30 minutes') ON CONFLICT(uid) DO UPDATE SET
 new_email=$2,old_email=$3,password_hash=$4,session_id=$5,mfa_version=$6,token_hash=$7,expires_at=EXCLUDED.expires_at`, uid, email, old, hash, sessionID, v.Version, hashToken(raw))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_jobs(uid,kind,recipient,token_hash,sealed_token,dedup_key,priority,expires_at)
 VALUES($1,'email_change',$2,$3,$4,'email_change:'||$3,10,now()+interval '30 minutes')`, uid, email, hashToken(raw), sealed)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail,ip) VALUES($1,$2,'email.change.request','',$3)`, uid, username, ip); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// The token is bound to the requesting session, password, MFA version and old address.
const emailChangeValid = `SELECT c.new_email,c.old_email FROM email_changes c JOIN users u ON u.id=c.uid
 JOIN sessions s ON s.id=c.session_id AND s.user_id=c.uid
 WHERE c.uid=$1 AND c.token_hash=$2 AND c.expires_at>now()
 AND u.email=c.old_email AND u.password_hash=c.password_hash
 AND NOT coalesce(u.blocked_until>now(),false) AND s.revoked_at IS NULL AND s.expires_at>now()
 AND c.mfa_version=coalesce((SELECT version FROM user_mfa WHERE user_id=u.id AND enabled),'')`

func (s *Store) ConfirmEmailChange(ctx context.Context, uid, sessionID int64, raw, ip string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	username, _, err := lockMFAUser(ctx, tx, uid)
	if err != nil {
		return err
	}
	var email, old string
	err = tx.QueryRow(ctx, emailChangeValid+` AND c.session_id=$3 FOR UPDATE OF c`, uid, hashToken(raw), sessionID).Scan(&email, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE users SET email=$2,email_verified=true WHERE id=$1`, uid, email)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrEmailUnavailable
		}
		return err
	}
	for _, q := range []string{
		`DELETE FROM email_changes WHERE uid=$1`,
		`DELETE FROM email_verifications WHERE uid=$1`,
		`UPDATE password_resets SET used=true WHERE uid=$1 AND NOT used`,
	} {
		if _, err = tx.Exec(ctx, q, uid); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, uid, sessionID); err != nil {
		return err
	}
	if err = invalidateMFAChallenges(ctx, tx, uid); err != nil {
		return err
	}
	if old != "" {
		_, err = tx.Exec(ctx, `INSERT INTO email_jobs(uid,kind,recipient,dedup_key,priority,expires_at) VALUES($1,'email_changed',$2,$3,10,now()+interval '7 days')`, uid, old, "email_changed:"+hashToken(raw))
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail,ip) VALUES($1,$2,'email.change.confirm','',$3)`, uid, username, ip); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
