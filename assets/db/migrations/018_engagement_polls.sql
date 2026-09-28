-- Polls and versioned operating rules for the engagement features.
CREATE TABLE IF NOT EXISTS engagement_config (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), version bigint NOT NULL DEFAULT 1,
 body jsonb NOT NULL
);
INSERT INTO engagement_config(id,body) VALUES(true,'{
 "poll":{"enabled":true,"maxOptions":10,"maxDays":30},
 "bounty":{"enabled":true,"minPoints":1,"maxPoints":10000,"maxDays":30},
 "checkin":{"enabled":true,"experience":5,"points":1,"timeZone":"Asia/Shanghai"}
}') ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS thread_polls (
 thread_id bigint PRIMARY KEY REFERENCES threads(id) ON DELETE CASCADE,
 question text NOT NULL CHECK(char_length(question) BETWEEN 1 AND 200),
 max_choices integer NOT NULL CHECK(max_choices BETWEEN 1 AND 20),
 state text NOT NULL CHECK(state IN ('pending','published','closed','rejected')),
 closes_at timestamptz NOT NULL, voters bigint NOT NULL DEFAULT 0 CHECK(voters>=0),
 created_at timestamptz NOT NULL DEFAULT now(), moderation_note text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS thread_polls_pending_idx ON thread_polls(thread_id) WHERE state='pending';
CREATE TABLE IF NOT EXISTS poll_options (
 thread_id bigint NOT NULL REFERENCES thread_polls(thread_id) ON DELETE CASCADE,
 position integer NOT NULL CHECK(position BETWEEN 1 AND 20),
 label text NOT NULL CHECK(char_length(label) BETWEEN 1 AND 200),
 votes bigint NOT NULL DEFAULT 0 CHECK(votes>=0), PRIMARY KEY(thread_id,position)
);
CREATE TABLE IF NOT EXISTS poll_ballots (
 thread_id bigint NOT NULL REFERENCES thread_polls(thread_id) ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE, choices integer[] NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(thread_id,user_id)
);
-- Extend each customized member matrix without overriding existing decisions.
UPDATE membership_config SET version=version+1,body=jsonb_set(body,'{levels}',(
 SELECT jsonb_agg(jsonb_set(l,'{permissions}',
  jsonb_build_object('poll.create',coalesce(l->'permissions'->'thread.create','false'::jsonb),
   'poll.vote',coalesce(l->'permissions'->'post.reply','false'::jsonb),
   'bounty.create',coalesce(l->'permissions'->'thread.create','false'::jsonb),
   'checkin.claim',true) || (l->'permissions')) ORDER BY ord)
 FROM jsonb_array_elements(body->'levels') WITH ORDINALITY AS x(l,ord)
)) WHERE EXISTS(SELECT 1 FROM jsonb_array_elements(body->'levels') l WHERE NOT (l->'permissions') ?& ARRAY['poll.create','poll.vote','bounty.create','checkin.claim']);
