CREATE TABLE IF NOT EXISTS user_mfa (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret_cipher text NOT NULL, enabled boolean NOT NULL DEFAULT false, enabled_at timestamptz,
 last_step bigint NOT NULL DEFAULT -1, recovery_hashes text[] NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mfa_challenges (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 password_hash text NOT NULL, expires_at timestamptz NOT NULL, used_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS mfa_challenges_expiry_idx ON mfa_challenges(expires_at);
