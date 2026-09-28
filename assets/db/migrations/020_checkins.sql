CREATE TABLE IF NOT EXISTS checkin_records (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 day date NOT NULL, streak integer NOT NULL CHECK(streak>=1),
 experience bigint NOT NULL CHECK(experience>=0), points bigint NOT NULL CHECK(points>=0),
 rule_version bigint NOT NULL, time_zone text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,day)
);
