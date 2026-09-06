-- Membership configuration, transactional event queue and audit ledgers.
CREATE TABLE IF NOT EXISTS membership_config (id boolean PRIMARY KEY DEFAULT true CHECK(id), version bigint NOT NULL DEFAULT 1, body jsonb NOT NULL);
INSERT INTO membership_config(id,body) VALUES(true, $config${
  "version": 1,
  "levels": [
    {
      "id": 0,
      "name": "新手会员",
      "rank": 0,
      "automatic": true,
      "experience": 0,
      "daysVisited": 0,
      "postsRead": 0,
      "postCount": 0,
      "emailVerified": false,
      "permissions": {
        "forum.read": true,
        "thread.create": true,
        "post.reply": true,
        "post.edit": true,
        "post.delete": true,
        "post.like": true,
        "thread.favorite": true,
        "post.report": true,
        "upload.image": true,
        "upload.file": true,
        "attachment.download": true,
        "post.link.direct": false,
        "post.skip.moderate": false
      },
      "limits": {
        "threadsPerDay": 100,
        "repliesPerDay": 100,
        "uploadsPerDay": 720,
        "uploadBytesPerDay": -1,
        "imageBytes": -1,
        "fileBytes": -1,
        "attachmentsPerPost": -1,
        "editMinutes": -1,
        "signatureLength": 300
      },
      "badge": {
        "label": "LV0",
        "icon": "seedling",
        "color": "#334155",
        "background": "#e2e8f0"
      }
    },
    {
      "id": 1,
      "name": "正式会员",
      "rank": 1,
      "automatic": true,
      "experience": 100,
      "daysVisited": 0,
      "postsRead": 0,
      "postCount": 0,
      "emailVerified": false,
      "permissions": {
        "forum.read": true,
        "thread.create": true,
        "post.reply": true,
        "post.edit": true,
        "post.delete": true,
        "post.like": true,
        "thread.favorite": true,
        "post.report": true,
        "upload.image": true,
        "upload.file": true,
        "attachment.download": true,
        "post.link.direct": true,
        "post.skip.moderate": false
      },
      "limits": {
        "threadsPerDay": 100,
        "repliesPerDay": 100,
        "uploadsPerDay": 720,
        "uploadBytesPerDay": -1,
        "imageBytes": -1,
        "fileBytes": -1,
        "attachmentsPerPost": -1,
        "editMinutes": -1,
        "signatureLength": 300
      },
      "badge": {
        "label": "LV1",
        "icon": "star",
        "color": "#334155",
        "background": "#e2e8f0"
      }
    },
    {
      "id": 2,
      "name": "活跃会员",
      "rank": 2,
      "automatic": true,
      "experience": 500,
      "daysVisited": 0,
      "postsRead": 0,
      "postCount": 0,
      "emailVerified": false,
      "permissions": {
        "forum.read": true,
        "thread.create": true,
        "post.reply": true,
        "post.edit": true,
        "post.delete": true,
        "post.like": true,
        "thread.favorite": true,
        "post.report": true,
        "upload.image": true,
        "upload.file": true,
        "attachment.download": true,
        "post.link.direct": true,
        "post.skip.moderate": true
      },
      "limits": {
        "threadsPerDay": 100,
        "repliesPerDay": 100,
        "uploadsPerDay": 720,
        "uploadBytesPerDay": -1,
        "imageBytes": -1,
        "fileBytes": -1,
        "attachmentsPerPost": -1,
        "editMinutes": -1,
        "signatureLength": 300
      },
      "badge": {
        "label": "LV2",
        "icon": "shield",
        "color": "#334155",
        "background": "#e2e8f0"
      }
    },
    {
      "id": 3,
      "name": "资深会员",
      "rank": 3,
      "automatic": true,
      "experience": 1500,
      "daysVisited": 0,
      "postsRead": 0,
      "postCount": 0,
      "emailVerified": false,
      "permissions": {
        "forum.read": true,
        "thread.create": true,
        "post.reply": true,
        "post.edit": true,
        "post.delete": true,
        "post.like": true,
        "thread.favorite": true,
        "post.report": true,
        "upload.image": true,
        "upload.file": true,
        "attachment.download": true,
        "post.link.direct": true,
        "post.skip.moderate": true
      },
      "limits": {
        "threadsPerDay": 100,
        "repliesPerDay": 100,
        "uploadsPerDay": 720,
        "uploadBytesPerDay": -1,
        "imageBytes": -1,
        "fileBytes": -1,
        "attachmentsPerPost": -1,
        "editMinutes": -1,
        "signatureLength": 300
      },
      "badge": {
        "label": "LV3",
        "icon": "gem",
        "color": "#334155",
        "background": "#e2e8f0"
      }
    },
    {
      "id": 4,
      "name": "核心会员",
      "rank": 4,
      "automatic": true,
      "experience": 5000,
      "daysVisited": 0,
      "postsRead": 0,
      "postCount": 0,
      "emailVerified": false,
      "permissions": {
        "forum.read": true,
        "thread.create": true,
        "post.reply": true,
        "post.edit": true,
        "post.delete": true,
        "post.like": true,
        "thread.favorite": true,
        "post.report": true,
        "upload.image": true,
        "upload.file": true,
        "attachment.download": true,
        "post.link.direct": true,
        "post.skip.moderate": true
      },
      "limits": {
        "threadsPerDay": 100,
        "repliesPerDay": 100,
        "uploadsPerDay": 720,
        "uploadBytesPerDay": -1,
        "imageBytes": -1,
        "fileBytes": -1,
        "attachmentsPerPost": -1,
        "editMinutes": -1,
        "signatureLength": 300
      },
      "badge": {
        "label": "LV4",
        "icon": "crown",
        "color": "#334155",
        "background": "#e2e8f0"
      }
    }
  ],
  "forums": [],
  "rules": {
    "active": {
      "enabled": true,
      "points": 1,
      "dailyCap": 1,
      "reverse": false
    },
    "thread": {
      "enabled": true,
      "points": 5,
      "dailyCap": 50,
      "reverse": true
    },
    "reply": {
      "enabled": true,
      "points": 2,
      "dailyCap": 40,
      "reverse": true
    },
    "like": {
      "enabled": true,
      "points": 1,
      "dailyCap": 20,
      "reverse": true
    },
    "digest": {
      "enabled": true,
      "points": 20,
      "dailyCap": 100,
      "reverse": true
    }
  },
  "guestPermissions": {
    "forum.read": true,
    "attachment.download": true
  }
}$config$::jsonb) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS member_states (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 level_id integer NOT NULL DEFAULT 0, experience bigint NOT NULL DEFAULT 0,
 locked boolean NOT NULL DEFAULT false, version bigint NOT NULL DEFAULT 1
);
INSERT INTO member_states(user_id) SELECT id FROM users ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS member_events (
 id bigserial PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL, source text NOT NULL, active boolean NOT NULL,
 rule jsonb NOT NULL, rule_version bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS member_experience (
 id bigserial PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 source text NOT NULL, kind text NOT NULL, delta bigint NOT NULL,
 reversed boolean NOT NULL DEFAULT false, reversible boolean NOT NULL DEFAULT true,
 rule_version bigint NOT NULL, reason text NOT NULL,
 actor_id bigint REFERENCES users(id) ON DELETE SET NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(user_id,source)
);
CREATE INDEX IF NOT EXISTS member_xp_user_time ON member_experience(user_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS member_daily (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE, day date NOT NULL,
 action text NOT NULL, amount bigint NOT NULL CHECK(amount>=0), PRIMARY KEY(user_id,day,action)
);
CREATE TABLE IF NOT EXISTS member_changes (
 id bigserial PRIMARY KEY, user_id bigint REFERENCES users(id) ON DELETE CASCADE,
 actor_id bigint REFERENCES users(id) ON DELETE SET NULL, action text NOT NULL,
 detail jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS member_read_posts (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 post_id bigint NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
 PRIMARY KEY(user_id,post_id)
);
CREATE OR REPLACE FUNCTION member_user_init() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO member_states(user_id) VALUES(NEW.id) ON CONFLICT DO NOTHING; RETURN NEW; END $$;
DROP TRIGGER IF EXISTS member_user_init ON users;
CREATE TRIGGER member_user_init AFTER INSERT ON users FOR EACH ROW EXECUTE FUNCTION member_user_init();
CREATE OR REPLACE FUNCTION member_enqueue(uid bigint, k text, src text, live boolean) RETURNS void LANGUAGE sql AS $$
 INSERT INTO member_events(user_id,kind,source,active,rule,rule_version)
 SELECT uid,k,src,live,body->'rules'->k,version FROM membership_config WHERE id AND EXISTS(SELECT 1 FROM users WHERE users.id=uid);
$$;
CREATE OR REPLACE FUNCTION member_post_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p posts; live boolean; a record;
BEGIN
 IF TG_OP='UPDATE' AND NEW.pending=OLD.pending AND NEW.deleted=OLD.deleted THEN RETURN NEW; END IF;
 IF TG_OP='DELETE' THEN p:=OLD; ELSE p:=NEW; END IF;
 SELECT TG_OP<>'DELETE' AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted INTO live FROM threads t WHERE t.id=p.thread_id;
 live:=coalesce(live,false);
 PERFORM member_enqueue(p.author_id,CASE WHEN p.floor=1 THEN 'thread' ELSE 'reply' END,'post:'||p.id,live);
 FOR a IN SELECT uid FROM post_actions WHERE pid=p.id AND action=1 AND uid<>p.author_id LOOP
  PERFORM member_enqueue(p.author_id,'like','like:'||p.id||':'||a.uid,live);
 END LOOP;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS member_post_event ON posts;
CREATE TRIGGER member_post_event AFTER INSERT OR UPDATE OR DELETE ON posts FOR EACH ROW EXECUTE FUNCTION member_post_event();
CREATE OR REPLACE FUNCTION member_thread_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE t threads; p record; a record; live boolean;
BEGIN
 IF TG_OP='UPDATE' AND NEW.pending=OLD.pending AND NEW.deleted=OLD.deleted AND NEW.digest=OLD.digest THEN RETURN NEW; END IF;
 IF TG_OP='DELETE' THEN t:=OLD; ELSE t:=NEW; END IF;
 live:=TG_OP<>'DELETE' AND NOT t.pending AND NOT t.deleted;
 PERFORM member_enqueue(t.author_id,'digest','digest:'||t.id,live AND t.digest);
 IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND (NEW.pending<>OLD.pending OR NEW.deleted<>OLD.deleted)) THEN
  FOR p IN SELECT * FROM posts WHERE thread_id=t.id LOOP
   PERFORM member_enqueue(p.author_id,CASE WHEN p.floor=1 THEN 'thread' ELSE 'reply' END,'post:'||p.id,live AND NOT p.pending AND NOT p.deleted);
   FOR a IN SELECT uid FROM post_actions WHERE pid=p.id AND action=1 AND uid<>p.author_id LOOP
    PERFORM member_enqueue(p.author_id,'like','like:'||p.id||':'||a.uid,live AND NOT p.pending AND NOT p.deleted);
   END LOOP;
  END LOOP;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS member_thread_event ON threads;
CREATE TRIGGER member_thread_event AFTER INSERT OR UPDATE OR DELETE ON threads FOR EACH ROW EXECUTE FUNCTION member_thread_event();
CREATE OR REPLACE FUNCTION member_like_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a post_actions; uid bigint; live boolean;
BEGIN
 IF TG_OP='DELETE' THEN a:=OLD; ELSE a:=NEW; END IF;
 IF a.action<>1 THEN RETURN NULL; END IF;
 SELECT p.author_id, TG_OP<>'DELETE' AND NOT p.pending AND NOT p.deleted AND NOT t.pending AND NOT t.deleted INTO uid,live FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=a.pid;
 IF uid IS NOT NULL AND uid<>a.uid THEN PERFORM member_enqueue(uid,'like','like:'||a.pid||':'||a.uid,live); END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS member_like_event ON post_actions;
CREATE TRIGGER member_like_event AFTER INSERT OR DELETE ON post_actions FOR EACH ROW EXECUTE FUNCTION member_like_event();

-- Eligibility changes (including email verification) are durable rechecks.
CREATE OR REPLACE FUNCTION member_user_progress() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.email_verified,NEW.post_count,NEW.days_visited,NEW.posts_read,NEW.blocked_until,NEW.banned_until)
 IS DISTINCT FROM ROW(OLD.email_verified,OLD.post_count,OLD.days_visited,OLD.posts_read,OLD.blocked_until,OLD.banned_until) THEN
  PERFORM member_enqueue(NEW.id,'active','recheck:'||NEW.id,false);
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS member_user_progress ON users;
CREATE TRIGGER member_user_progress AFTER UPDATE OF email_verified,post_count,days_visited,posts_read,blocked_until,banned_until ON users FOR EACH ROW EXECUTE FUNCTION member_user_progress();

ALTER TABLE users DROP COLUMN IF EXISTS trust_level;
