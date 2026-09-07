-- Durable email outbox. Tokens are encrypted with an application-owned key.
CREATE TABLE IF NOT EXISTS email_jobs (
 id bigserial PRIMARY KEY,
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('password_reset','email_verify','mention','reply','reply.direct','subscription')),
 template_version integer NOT NULL DEFAULT 1 CHECK(template_version=1),
 recipient text NOT NULL,
 post_id bigint REFERENCES posts(id) ON DELETE CASCADE,
 token_hash text NOT NULL DEFAULT '',
 sealed_token text NOT NULL DEFAULT '',
 dedup_key text NOT NULL UNIQUE,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','dead','cancelled')),
 priority integer NOT NULL DEFAULT 0,
 attempts integer NOT NULL DEFAULT 0,
 retries integer NOT NULL DEFAULT 0,
 version bigint NOT NULL DEFAULT 1,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 expires_at timestamptz NOT NULL,
 sent_at timestamptz,
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS email_jobs_pending_idx ON email_jobs(priority DESC,next_attempt_at,id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS email_jobs_lease_idx ON email_jobs(lease_until) WHERE status='sending';
