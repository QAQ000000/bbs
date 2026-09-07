-- Transactional MFA enrollment and browser-bound login challenges.
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS version text NOT NULL DEFAULT '';
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS setup_session bigint;
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS setup_password text;
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS setup_expires timestamptz;
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS token_hash text;
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS csrf_hash text NOT NULL DEFAULT '';
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS mfa_version text NOT NULL DEFAULT '';
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS mfa_challenges_token_idx ON mfa_challenges(token_hash);
CREATE TABLE IF NOT EXISTS mfa_attempts (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 scope text NOT NULL,
 window_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 1,
 PRIMARY KEY(user_id,scope)
);
-- Legacy challenges cannot be completed; active secrets remain protected.
UPDATE mfa_challenges SET used_at=now() WHERE token_hash IS NULL AND used_at IS NULL;
UPDATE user_mfa SET recovery_hashes='{}' WHERE EXISTS (
 SELECT 1 FROM unnest(recovery_hashes) h WHERE length(h)<>64
);
