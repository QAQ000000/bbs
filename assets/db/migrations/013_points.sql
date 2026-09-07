-- Independent points accounts and append-only accounting entries.
CREATE TABLE IF NOT EXISTS points_config (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), version bigint NOT NULL DEFAULT 1,
 body jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS points_accounts (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 balance bigint NOT NULL DEFAULT 0 CHECK(balance BETWEEN -9000000000000 AND 9000000000000),
 frozen bigint NOT NULL DEFAULT 0 CHECK(frozen BETWEEN 0 AND 9000000000000),
 version bigint NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS points_ledger (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id),
 source text NOT NULL, kind text NOT NULL,
 delta bigint NOT NULL, frozen_delta bigint NOT NULL DEFAULT 0,
 balance_after bigint NOT NULL, frozen_after bigint NOT NULL,
 rule_version bigint NOT NULL, reversible boolean NOT NULL DEFAULT false,
 reason text NOT NULL, actor_id bigint NOT NULL DEFAULT 0,
 event_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(user_id,source)
);
CREATE INDEX IF NOT EXISTS points_ledger_user_idx ON points_ledger(user_id,id DESC);
CREATE INDEX IF NOT EXISTS points_ledger_daily_idx ON points_ledger(user_id,kind,event_at) WHERE delta>0;
CREATE OR REPLACE FUNCTION points_ledger_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'points ledger is append-only'; END $$;
DROP TRIGGER IF EXISTS points_ledger_immutable ON points_ledger;
CREATE TRIGGER points_ledger_immutable BEFORE UPDATE OR DELETE ON points_ledger FOR EACH ROW EXECUTE FUNCTION points_ledger_immutable();

-- A one-time baseline prevents old content being toggled to collect a new reward.
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM points_config WHERE id) THEN
  INSERT INTO points_config(id,body) VALUES(true,'{
   "rules":{
    "active":{"enabled":false,"points":0,"dailyCap":0,"reverse":false},
    "thread":{"enabled":true,"points":1,"dailyCap":10,"reverse":true},
    "reply":{"enabled":true,"points":1,"dailyCap":20,"reverse":true},
    "like":{"enabled":true,"points":1,"dailyCap":20,"reverse":true},
    "digest":{"enabled":true,"points":5,"dailyCap":25,"reverse":true},
    "accepted":{"enabled":true,"points":10,"dailyCap":50,"reverse":true}
   },"acceptedExperience":{"enabled":true,"points":30,"dailyCap":150,"reverse":true}
  }');
  INSERT INTO points_ledger(user_id,source,kind,delta,balance_after,frozen_after,rule_version,reason,event_at)
  SELECT user_id,source,'baseline',0,0,0,0,'pre-points baseline',now() FROM (
   SELECT user_id,source FROM member_experience WHERE source NOT LIKE 'reverse:%'
   UNION SELECT author_id,'post:'||id FROM posts WHERE NOT pending
   UNION SELECT author_id,'digest:'||id FROM threads WHERE digest
   UNION SELECT p.author_id,'like:'||p.id||':'||a.uid FROM post_actions a JOIN posts p ON p.id=a.pid WHERE a.action=1
   UNION SELECT p.author_id,'accepted:'||p.id FROM accepted_replies a JOIN posts p ON p.id=a.post_id
  ) x ON CONFLICT DO NOTHING;
 END IF;
END $$;

ALTER TABLE member_events ADD COLUMN IF NOT EXISTS points_rule jsonb;
ALTER TABLE member_events ADD COLUMN IF NOT EXISTS points_version bigint;
CREATE OR REPLACE FUNCTION points_snapshot_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT body->'rules'->NEW.kind,version INTO NEW.points_rule,NEW.points_version FROM points_config WHERE id;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS points_snapshot_event ON member_events;
CREATE TRIGGER points_snapshot_event BEFORE INSERT ON member_events FOR EACH ROW EXECUTE FUNCTION points_snapshot_event();

-- Acceptance adds an experience event and uses the same points snapshot/worker.
CREATE OR REPLACE FUNCTION points_accept_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE pid bigint; uid bigint; live boolean;
BEGIN
 IF TG_OP='DELETE' THEN pid:=OLD.post_id; ELSE pid:=NEW.post_id; END IF;
 SELECT p.author_id,TG_OP<>'DELETE' AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted AND p.author_id<>t.author_id
 INTO uid,live FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=pid;
 IF uid IS NOT NULL THEN
  INSERT INTO member_events(user_id,kind,source,active,rule,rule_version)
  SELECT uid,'accepted','accepted:'||pid,live,body->'acceptedExperience',version FROM points_config WHERE id;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS points_accept_event ON accepted_replies;
CREATE TRIGGER points_accept_event AFTER INSERT OR DELETE ON accepted_replies FOR EACH ROW EXECUTE FUNCTION points_accept_event();
