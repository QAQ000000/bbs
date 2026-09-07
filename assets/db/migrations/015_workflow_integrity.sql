-- Pending email changes never replace the recovery address before proof of ownership.
CREATE TABLE IF NOT EXISTS email_changes (
 uid bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 new_email text NOT NULL,
 old_email text NOT NULL,
 password_hash text NOT NULL,
 session_id bigint NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 mfa_version text NOT NULL DEFAULT '',
 token_hash text NOT NULL UNIQUE,
 expires_at timestamptz NOT NULL
);
ALTER TABLE email_jobs DROP CONSTRAINT IF EXISTS email_jobs_kind_check;
ALTER TABLE email_jobs ADD CONSTRAINT email_jobs_kind_check CHECK(kind IN
 ('password_reset','email_verify','email_change','email_changed','mention','reply','reply.direct','subscription'));

-- Older workers may have completed hidden events without delivery. Resume at their
-- saved cursor; the original timestamp and delivery receipts prevent retroactive duplicates.
UPDATE subscription_events SET completed=false WHERE completed;
