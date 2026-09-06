-- Task-based honorary titles, independent of membership permissions.
CREATE TABLE IF NOT EXISTS titles (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 version bigint NOT NULL DEFAULT 1,
 body jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS user_titles (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title_id bigint NOT NULL REFERENCES titles(id),
 status text NOT NULL CHECK(status IN ('earned','revoked')),
 source text NOT NULL CHECK(source IN ('automatic','manual')),
 rule_version bigint NOT NULL,
 earned_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz,
 PRIMARY KEY(user_id,title_id)
);
CREATE TABLE IF NOT EXISTS title_equipment (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 title_id bigint NOT NULL,
 FOREIGN KEY(user_id,title_id) REFERENCES user_titles(user_id,title_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS title_progress (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title_id bigint NOT NULL REFERENCES titles(id),
 rule_version bigint NOT NULL,
 counts jsonb NOT NULL,
 checked_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,title_id)
);
CREATE TABLE IF NOT EXISTS title_logs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 title_id bigint NOT NULL REFERENCES titles(id),
 user_id bigint REFERENCES users(id) ON DELETE SET NULL,
 actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
 action text NOT NULL,
 detail jsonb NOT NULL,
 request_key text,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS title_log_request ON title_logs(actor_id,request_key) WHERE request_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS title_log_title ON title_logs(title_id,id DESC);
CREATE TABLE IF NOT EXISTS title_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS title_events_user ON title_events(user_id);
CREATE TABLE IF NOT EXISTS title_jobs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 title_id bigint NOT NULL REFERENCES titles(id),
 rule_version bigint NOT NULL,
 cursor_id bigint NOT NULL DEFAULT 0,
 max_user_id bigint NOT NULL,
 processed bigint NOT NULL DEFAULT 0,
 awarded bigint NOT NULL DEFAULT 0,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','complete','superseded')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS title_jobs_pending ON title_jobs(id) WHERE status='pending';
CREATE TABLE IF NOT EXISTS title_schedule (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 next_run timestamptz NOT NULL DEFAULT now()
);
INSERT INTO title_schedule(id) VALUES(true) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS accepted_replies (
 thread_id bigint PRIMARY KEY REFERENCES threads(id) ON DELETE CASCADE,
 post_id bigint NOT NULL UNIQUE REFERENCES posts(id) ON DELETE CASCADE,
 accepted_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS acceptance_logs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 thread_id bigint NOT NULL,
 post_id bigint,
 actor_id bigint NOT NULL,
 accepted boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS title_posts_author ON posts(author_id,thread_id) WHERE NOT deleted AND NOT pending;

-- Append-only invalidations never contend with a worker holding a user row.
CREATE OR REPLACE FUNCTION title_enqueue(uid bigint) RETURNS void LANGUAGE sql AS $$
 INSERT INTO title_events(user_id) SELECT id FROM users WHERE id=uid;
$$;
CREATE OR REPLACE FUNCTION title_post_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND ROW(NEW.pending,NEW.deleted,NEW.author_id,NEW.thread_id)
    IS NOT DISTINCT FROM ROW(OLD.pending,OLD.deleted,OLD.author_id,OLD.thread_id) THEN RETURN NULL; END IF;
 IF TG_OP<>'INSERT' THEN PERFORM title_enqueue(OLD.author_id); END IF;
 IF TG_OP<>'DELETE' THEN PERFORM title_enqueue(NEW.author_id); END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW.pending OR NEW.deleted THEN DELETE FROM accepted_replies WHERE post_id=NEW.id; END IF;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS title_post_event ON posts;
CREATE TRIGGER title_post_event AFTER INSERT OR UPDATE OR DELETE ON posts FOR EACH ROW EXECUTE FUNCTION title_post_event();
CREATE OR REPLACE FUNCTION title_thread_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tid bigint;
BEGIN
 IF TG_OP='UPDATE' AND ROW(NEW.pending,NEW.deleted,NEW.digest,NEW.forum_id)
    IS NOT DISTINCT FROM ROW(OLD.pending,OLD.deleted,OLD.digest,OLD.forum_id) THEN RETURN NULL; END IF;
 IF TG_OP='DELETE' THEN tid:=OLD.id; ELSE tid:=NEW.id; END IF;
 INSERT INTO title_events(user_id) SELECT DISTINCT author_id FROM posts WHERE thread_id=tid;
 IF TG_OP='UPDATE' THEN
  IF NEW.pending OR NEW.deleted THEN DELETE FROM accepted_replies WHERE thread_id=NEW.id; END IF;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS title_thread_event ON threads;
CREATE TRIGGER title_thread_event AFTER INSERT OR UPDATE OR DELETE ON threads FOR EACH ROW EXECUTE FUNCTION title_thread_event();
CREATE OR REPLACE FUNCTION title_like_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE pid bigint; kind smallint;
BEGIN
 IF TG_OP='DELETE' THEN pid:=OLD.pid; kind:=OLD.action; ELSE pid:=NEW.pid; kind:=NEW.action; END IF;
 IF kind=1 THEN INSERT INTO title_events(user_id) SELECT author_id FROM posts WHERE id=pid; END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS title_like_event ON post_actions;
CREATE TRIGGER title_like_event AFTER INSERT OR DELETE ON post_actions FOR EACH ROW EXECUTE FUNCTION title_like_event();
CREATE OR REPLACE FUNCTION title_accept_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN INSERT INTO title_events(user_id) SELECT author_id FROM posts WHERE id=OLD.post_id; END IF;
 IF TG_OP<>'DELETE' THEN INSERT INTO title_events(user_id) SELECT author_id FROM posts WHERE id=NEW.post_id; END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS title_accept_event ON accepted_replies;
CREATE TRIGGER title_accept_event AFTER INSERT OR UPDATE OR DELETE ON accepted_replies FOR EACH ROW EXECUTE FUNCTION title_accept_event();
CREATE OR REPLACE FUNCTION title_user_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM title_enqueue(NEW.id); RETURN NULL; END $$;
DROP TRIGGER IF EXISTS title_user_event ON users;
CREATE TRIGGER title_user_event AFTER INSERT OR UPDATE OF days_visited,email_verified,banned_until,blocked_until ON users FOR EACH ROW EXECUTE FUNCTION title_user_event();
CREATE OR REPLACE FUNCTION title_member_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM title_enqueue(NEW.user_id); RETURN NULL; END $$;
DROP TRIGGER IF EXISTS title_member_event ON member_states;
CREATE TRIGGER title_member_event AFTER INSERT OR UPDATE OF experience,level_id ON member_states FOR EACH ROW EXECUTE FUNCTION title_member_event();
