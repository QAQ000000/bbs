-- Keep the thread identity after a physical purge so escrow can still be refunded.
CREATE TABLE IF NOT EXISTS thread_bounties (
 thread_id bigint PRIMARY KEY,
 owner_id bigint NOT NULL REFERENCES users(id),
 amount bigint NOT NULL CHECK(amount BETWEEN 1 AND 1000000),
 duration_hours integer NOT NULL CHECK(duration_hours BETWEEN 1 AND 8760),
 state text NOT NULL DEFAULT 'active' CHECK(state IN ('active','awarded','canceled','expired')),
 closes_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), settled_at timestamptz,
 recipient_id bigint REFERENCES users(id), post_id bigint,
 rule_version bigint NOT NULL, note text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS thread_bounties_expiry_idx ON thread_bounties(closes_at,thread_id) WHERE state='active';
CREATE INDEX IF NOT EXISTS thread_bounties_owner_idx ON thread_bounties(owner_id,thread_id);
