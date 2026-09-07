-- Device sessions: public identifiers are independent of authentication tokens.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS id bigserial;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device_name text NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS user_agent text NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS masked_ip text NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS last_seen_at timestamptz;
UPDATE sessions SET last_seen_at=created_at WHERE last_seen_at IS NULL;
ALTER TABLE sessions ALTER COLUMN last_seen_at SET DEFAULT now();
ALTER TABLE sessions ALTER COLUMN last_seen_at SET NOT NULL;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS revoked_at timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS sessions_id_idx ON sessions(id);
CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id,id DESC);
