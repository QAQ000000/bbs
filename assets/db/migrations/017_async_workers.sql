-- Durable derived data. Existing pending work must survive migration replay.
CREATE TABLE IF NOT EXISTS forum_stat_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 forum_id bigint NOT NULL REFERENCES forums(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 16),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_sqlstate text NOT NULL DEFAULT '' CHECK(length(last_sqlstate)<=5)
);
CREATE INDEX IF NOT EXISTS forum_stat_events_due ON forum_stat_events(next_attempt_at,id);
CREATE INDEX IF NOT EXISTS forum_stat_events_forum ON forum_stat_events(forum_id,id);
CREATE TABLE IF NOT EXISTS search_index_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 post_id bigint NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 16),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_sqlstate text NOT NULL DEFAULT '' CHECK(length(last_sqlstate)<=5),
 UNIQUE(post_id)
);
CREATE INDEX IF NOT EXISTS search_index_events_due ON search_index_events(next_attempt_at,id);
CREATE TABLE IF NOT EXISTS analytics_snapshots (
 name text NOT NULL,
 period_start timestamptz NOT NULL,
 generated_at timestamptz NOT NULL DEFAULT now(),
 payload jsonb NOT NULL,
 PRIMARY KEY(name, period_start)
);
CREATE INDEX IF NOT EXISTS analytics_snapshots_latest ON analytics_snapshots(name, generated_at DESC);
