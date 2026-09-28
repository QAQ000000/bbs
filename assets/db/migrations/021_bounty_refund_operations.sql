-- Refund failures survive worker restarts; terminal financial states are unchanged.
ALTER TABLE thread_bounties ADD COLUMN IF NOT EXISTS refund_attempts integer NOT NULL DEFAULT 0 CHECK(refund_attempts>=0);
ALTER TABLE thread_bounties ADD COLUMN IF NOT EXISTS refund_error_code text NOT NULL DEFAULT '';
ALTER TABLE thread_bounties ADD COLUMN IF NOT EXISTS refund_failed_at timestamptz;
ALTER TABLE thread_bounties ADD COLUMN IF NOT EXISTS refund_next_attempt_at timestamptz;
CREATE INDEX IF NOT EXISTS thread_bounties_refund_retry_idx ON thread_bounties(refund_next_attempt_at,thread_id) WHERE state='active' AND refund_error_code<>'';
