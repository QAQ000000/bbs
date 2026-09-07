package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"dzforum/internal/mfa"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrMFARequired    = errors.New("MFA required")
	ErrMFAInvalid     = errors.New("invalid MFA proof or state")
	ErrMFALimited     = errors.New("too many MFA attempts")
	ErrMFAUnavailable = errors.New("MFA key unavailable or incorrect")
)

type MFAState struct {
	Enabled        bool
	SecretCipher   string
	LastStep       int64
	RecoveryHashes []string
	Version        string
}

func (s *Store) MFA(ctx context.Context, uid int64) (MFAState, error) {
	var v MFAState
	err := s.pool.QueryRow(ctx, `SELECT enabled,secret_cipher,last_step,recovery_hashes,version FROM user_mfa WHERE user_id=$1`, uid).Scan(&v.Enabled, &v.SecretCipher, &v.LastStep, &v.RecoveryHashes, &v.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}

// Every security mutation locks the user first, then checks current credentials.
func lockMFAUser(ctx context.Context, tx pgx.Tx, uid int64) (username, hash string, err error) {
	err = tx.QueryRow(ctx, `SELECT username,password_hash FROM users WHERE id=$1 AND NOT coalesce(blocked_until>now(),false) FOR UPDATE`, uid).Scan(&username, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrMFAInvalid
	}
	return
}

func mfaAudit(ctx context.Context, tx pgx.Tx, uid int64, username, action, ip string) error {
	_, err := tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail,ip) VALUES($1,$2,$3,'',$4)`, uid, username, "mfa."+action, ip)
	return err
}

// Counters persist failed proofs and are shared by every server process.
func mfaAttempt(ctx context.Context, tx pgx.Tx, uid int64, scope string, limit int, window string) error {
	var n int
	err := tx.QueryRow(ctx, `INSERT INTO mfa_attempts(user_id,scope) VALUES($1,$2)
 ON CONFLICT(user_id,scope) DO UPDATE SET
 attempts=CASE WHEN mfa_attempts.window_at<=now()-$3::interval THEN 1 ELSE least(mfa_attempts.attempts+1,$4+1) END,
 window_at=CASE WHEN mfa_attempts.window_at<=now()-$3::interval THEN now() ELSE mfa_attempts.window_at END RETURNING attempts`, uid, scope, window, limit).Scan(&n)
	if err != nil {
		return err
	}
	if n > limit {
		return ErrMFALimited
	}
	return nil
}

func mfaReject(ctx context.Context, tx pgx.Tx, uid int64, username, ip string, reason error) error {
	if err := mfaAudit(ctx, tx, uid, username, "failure", ip); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return reason
}

func invalidateMFAChallenges(ctx context.Context, tx pgx.Tx, uid int64) error {
	_, err := tx.Exec(ctx, `UPDATE mfa_challenges SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, uid)
	return err
}

type MFAEnrollment struct {
	Secret        string   `json:"secret,omitempty"`
	SetupID       string   `json:"setupId,omitempty"`
	RecoveryCodes []string `json:"recoveryCodes,omitempty"`
	Enabled       bool     `json:"enabled"`
}

// ManageMFA authenticates the caller again while holding the account lock.
func (s *Store) ManageMFA(ctx context.Context, uid, sessionID int64, action, password, setupID, code, recovery, ip string, cipher *mfa.Cipher) (MFAEnrollment, error) {
	var out MFAEnrollment
	if cipher == nil {
		return out, ErrMFAUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	username, hash, err := lockMFAUser(ctx, tx, uid)
	if err != nil {
		return out, err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>now())`, sessionID, uid).Scan(&active); err != nil {
		return out, err
	}
	if !active {
		return out, ErrMFAInvalid
	}
	if err = mfaAttempt(ctx, tx, uid, "manage", 10, "10 minutes"); err != nil {
		if errors.Is(err, ErrMFALimited) {
			err = mfaReject(ctx, tx, uid, username, ip, err)
		}
		return out, err
	}
	reject := func() (MFAEnrollment, error) {
		return MFAEnrollment{}, mfaReject(ctx, tx, uid, username, ip, ErrMFAInvalid)
	}
	if len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return reject()
	}
	var v MFAState
	var setupValid bool
	err = tx.QueryRow(ctx, `SELECT enabled,secret_cipher,last_step,recovery_hashes,version,
 coalesce(setup_session=$2 AND setup_password=$3 AND setup_expires>now(),false)
 FROM user_mfa WHERE user_id=$1 FOR UPDATE`, uid, sessionID, hash).Scan(&v.Enabled, &v.SecretCipher, &v.LastStep, &v.RecoveryHashes, &v.Version, &setupValid)
	exists := !errors.Is(err, pgx.ErrNoRows)
	if err != nil && exists {
		return out, err
	}
	if action == "setup" {
		if v.Enabled {
			return reject()
		}
		out.Secret, err = mfa.NewSecret()
		if err != nil {
			return out, err
		}
		sealed, e := cipher.Seal(out.Secret)
		if e != nil {
			return MFAEnrollment{}, ErrMFAUnavailable
		}
		out.SetupID = newToken(32)
		_, err = tx.Exec(ctx, `INSERT INTO user_mfa(user_id,secret_cipher,version,setup_session,setup_password,setup_expires)
   VALUES($1,$2,$3,$4,$5,now()+interval '10 minutes') ON CONFLICT(user_id) DO UPDATE SET
   secret_cipher=$2,version=$3,setup_session=$4,setup_password=$5,setup_expires=now()+interval '10 minutes',last_step=-1,recovery_hashes='{}',updated_at=now()`, uid, sealed, out.SetupID, sessionID, hash)
	} else {
		if !exists || (action == "enable" && (v.Enabled || !setupValid || setupID != v.Version || recovery != "")) || (action != "enable" && !v.Enabled) {
			return reject()
		}
		secret, e := cipher.Open(v.SecretCipher)
		if e != nil {
			return out, ErrMFAUnavailable
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return out, err
		}
		step, hashes, used, ok := mfaProof(v, secret, code, recovery, now)
		if !ok {
			return reject()
		}
		switch action {
		case "enable", "recovery.rotate":
			out.RecoveryCodes, hashes, err = mfa.RecoveryCodes()
			if err != nil {
				return out, err
			}
			out.Enabled = true
			_, err = tx.Exec(ctx, `UPDATE user_mfa SET enabled=true,enabled_at=coalesce(enabled_at,now()),last_step=$2,recovery_hashes=$3,version=$4,setup_session=NULL,setup_password=NULL,setup_expires=NULL,updated_at=now() WHERE user_id=$1`, uid, step, hashes, newToken(32))
		case "disable":
			_, err = tx.Exec(ctx, `DELETE FROM user_mfa WHERE user_id=$1`, uid)
		default:
			return out, ErrMFAInvalid
		}
		if err == nil && used {
			err = mfaAudit(ctx, tx, uid, username, "recovery.used", ip)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, uid, sessionID)
		}
		if err == nil {
			err = invalidateMFAChallenges(ctx, tx, uid)
		}
	}
	if err != nil {
		return MFAEnrollment{}, err
	}
	if err = mfaAudit(ctx, tx, uid, username, action, ip); err != nil {
		return MFAEnrollment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MFAEnrollment{}, err
	}
	return out, nil
}

func mfaProof(v MFAState, secret, code, recovery string, now time.Time) (int64, []string, bool, bool) {
	if (code == "") == (recovery == "") {
		return v.LastStep, nil, false, false
	}
	if code != "" {
		step, ok := mfa.Verify(secret, code, now, v.LastStep)
		return step, v.RecoveryHashes, false, ok
	}
	h := mfa.HashRecovery(recovery)
	if h != "" {
		for i, stored := range v.RecoveryHashes {
			if subtle.ConstantTimeCompare([]byte(h), []byte(stored)) == 1 {
				hashes := append([]string{}, v.RecoveryHashes[:i]...)
				hashes = append(hashes, v.RecoveryHashes[i+1:]...)
				return v.LastStep, hashes, true, true
			}
		}
	}
	return v.LastStep, nil, false, false
}

func (s *Store) CreateMFAChallenge(ctx context.Context, uid int64, verifiedHash, binding string) (string, error) {
	if binding == "" {
		return "", ErrMFAInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	username, hash, err := lockMFAUser(ctx, tx, uid)
	if err != nil {
		return "", err
	}
	if hash != verifiedHash {
		return "", ErrMFAInvalid
	}
	var version string
	if err = tx.QueryRow(ctx, `SELECT version FROM user_mfa WHERE user_id=$1 AND enabled`, uid).Scan(&version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrMFAInvalid
		}
		return "", err
	}
	if err = mfaAttempt(ctx, tx, uid, "challenge", 5, "5 minutes"); err != nil {
		if errors.Is(err, ErrMFALimited) {
			err = mfaReject(ctx, tx, uid, username, "", err)
		}
		return "", err
	}
	raw := newToken(32)
	_, err = tx.Exec(ctx, `INSERT INTO mfa_challenges(user_id,password_hash,expires_at,token_hash,csrf_hash,mfa_version) VALUES($1,$2,now()+interval '5 minutes',$3,$4,$5)`, uid, hash, hashToken(raw), hashToken(binding), version)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return raw, nil
}

// Proof consumption, challenge completion and session creation commit together.
func (s *Store) CompleteMFA(ctx context.Context, challenge, binding, code, recovery, ua, ip string, cipher *mfa.Cipher) (uid int64, token, csrf string, err error) {
	if cipher == nil {
		return 0, "", "", ErrMFAUnavailable
	}
	if len(challenge) != 43 || binding == "" {
		return 0, "", "", ErrMFAInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, "", "", err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT user_id FROM mfa_challenges WHERE token_hash=$1`, hashToken(challenge)).Scan(&uid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrMFAInvalid
		}
		return 0, "", "", err
	}
	username, hash, err := lockMFAUser(ctx, tx, uid)
	if err != nil {
		return 0, "", "", err
	}
	var version string
	var valid bool
	err = tx.QueryRow(ctx, `SELECT mfa_version,used_at IS NULL AND expires_at>now() AND attempts<5 AND password_hash=$2 AND csrf_hash=$3 FROM mfa_challenges WHERE token_hash=$1 FOR UPDATE`, hashToken(challenge), hash, hashToken(binding)).Scan(&version, &valid)
	if err != nil {
		return 0, "", "", err
	}
	if !valid {
		return 0, "", "", ErrMFAInvalid
	}
	var v MFAState
	err = tx.QueryRow(ctx, `SELECT enabled,secret_cipher,last_step,recovery_hashes,version FROM user_mfa WHERE user_id=$1 FOR UPDATE`, uid).Scan(&v.Enabled, &v.SecretCipher, &v.LastStep, &v.RecoveryHashes, &v.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", "", ErrMFAInvalid
	}
	if err != nil {
		return 0, "", "", err
	}
	if !v.Enabled || v.Version != version {
		return 0, "", "", ErrMFAInvalid
	}
	secret, e := cipher.Open(v.SecretCipher)
	if e != nil {
		return 0, "", "", ErrMFAUnavailable
	}
	if err = mfaAttempt(ctx, tx, uid, "verify", 10, "10 minutes"); err != nil {
		if errors.Is(err, ErrMFALimited) {
			err = mfaReject(ctx, tx, uid, username, ip, err)
		}
		return 0, "", "", err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return 0, "", "", err
	}
	step, hashes, used, ok := mfaProof(v, secret, code, recovery, now)
	if !ok {
		if _, err = tx.Exec(ctx, `UPDATE mfa_challenges SET attempts=attempts+1 WHERE token_hash=$1`, hashToken(challenge)); err != nil {
			return 0, "", "", err
		}
		return 0, "", "", mfaReject(ctx, tx, uid, username, ip, ErrMFAInvalid)
	}
	if _, err = tx.Exec(ctx, `UPDATE user_mfa SET last_step=$2,recovery_hashes=$3,updated_at=now() WHERE user_id=$1`, uid, step, hashes); err != nil {
		return 0, "", "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE mfa_challenges SET used_at=now() WHERE token_hash=$1`, hashToken(challenge)); err != nil {
		return 0, "", "", err
	}
	token, csrf, err = createDeviceSessionTx(ctx, tx, uid, ua, ip)
	if err != nil {
		return 0, "", "", err
	}
	if used {
		if err = mfaAudit(ctx, tx, uid, username, "recovery.used", ip); err != nil {
			return 0, "", "", err
		}
	}
	if err = mfaAudit(ctx, tx, uid, username, "login", ip); err != nil {
		return 0, "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, "", "", err
	}
	return uid, token, csrf, nil
}

func (s *Store) PurgeMFA(ctx context.Context) error {
	// Each batch is bounded; enrollment expiry is also enforced on every request.
	for _, q := range []string{
		`DELETE FROM mfa_challenges WHERE id IN (SELECT id FROM mfa_challenges WHERE expires_at<now() OR used_at<now()-interval '1 day' LIMIT 1000)`,
		`DELETE FROM user_mfa WHERE NOT enabled AND (setup_expires<now() OR setup_expires IS NULL) AND user_id IN (SELECT user_id FROM user_mfa WHERE NOT enabled AND (setup_expires<now() OR setup_expires IS NULL) LIMIT 1000)`,
		`DELETE FROM mfa_attempts WHERE window_at<now()-interval '1 day' AND (user_id,scope) IN (SELECT user_id,scope FROM mfa_attempts WHERE window_at<now()-interval '1 day' LIMIT 1000)`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
