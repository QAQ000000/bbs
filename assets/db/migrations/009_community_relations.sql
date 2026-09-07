-- Community relations: follows, tags, subscriptions and direct messages.
CREATE TABLE IF NOT EXISTS user_follows (
 follower_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 following_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (follower_id, following_id),
 CHECK (follower_id <> following_id)
);
CREATE INDEX IF NOT EXISTS user_follows_following_idx ON user_follows(following_id, created_at DESC);

CREATE TABLE IF NOT EXISTS tags (
 id bigserial PRIMARY KEY,
 name text NOT NULL,
 slug text NOT NULL UNIQUE,
 description text NOT NULL DEFAULT '',
 color text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
 created_by bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS thread_tags (
 thread_id bigint NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
 tag_id bigint NOT NULL REFERENCES tags(id) ON DELETE RESTRICT,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(thread_id,tag_id)
);
CREATE INDEX IF NOT EXISTS thread_tags_tag_idx ON thread_tags(tag_id,thread_id DESC);
CREATE TABLE IF NOT EXISTS tag_aliases (
 alias text PRIMARY KEY,
 tag_id bigint NOT NULL REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS thread_subscriptions (
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 thread_id bigint NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT true,
 notify_in_app boolean NOT NULL DEFAULT true,
 notify_email boolean NOT NULL DEFAULT true,
 muted_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(uid,thread_id)
);
CREATE TABLE IF NOT EXISTS forum_subscriptions (
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 forum_id bigint NOT NULL REFERENCES forums(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT true,
 notify_in_app boolean NOT NULL DEFAULT true,
 notify_email boolean NOT NULL DEFAULT true,
 muted_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(uid,forum_id)
);
CREATE TABLE IF NOT EXISTS tag_subscriptions (
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 tag_id bigint NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT true,
 notify_in_app boolean NOT NULL DEFAULT true,
 notify_email boolean NOT NULL DEFAULT true,
 muted_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(uid,tag_id)
);

CREATE TABLE IF NOT EXISTS conversations (
 id bigserial PRIMARY KEY,
 kind text NOT NULL DEFAULT 'direct' CHECK (kind='direct'),
 created_at timestamptz NOT NULL DEFAULT now(),
 last_message_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS conversation_members (
 conversation_id bigint NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 blocked boolean NOT NULL DEFAULT false,
 blocked_at timestamptz,
 last_read_message_id bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(conversation_id,uid)
);
CREATE TABLE IF NOT EXISTS conversation_pair_states (
 conversation_id bigint PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
 initiator_id bigint NOT NULL REFERENCES users(id),
 recipient_id bigint NOT NULL REFERENCES users(id),
 first_message_id bigint NOT NULL DEFAULT 0,
 recipient_replied_at timestamptz,
 unrestricted_at timestamptz,
 CHECK(initiator_id <> recipient_id)
);
CREATE TABLE IF NOT EXISTS messages (
 id bigserial PRIMARY KEY,
 conversation_id bigint NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 sender_id bigint NOT NULL REFERENCES users(id),
 body text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 deleted_at timestamptz
);
CREATE INDEX IF NOT EXISTS messages_conversation_idx ON messages(conversation_id,id);

-- Complete relation constraints and durable subscription fan-out.
CREATE UNIQUE INDEX IF NOT EXISTS conversation_pair_unique
 ON conversation_pair_states(least(initiator_id,recipient_id),greatest(initiator_id,recipient_id));
CREATE INDEX IF NOT EXISTS conversation_members_uid_idx ON conversation_members(uid,conversation_id);
CREATE INDEX IF NOT EXISTS messages_sender_time_idx ON messages(sender_id,created_at);
CREATE UNIQUE INDEX IF NOT EXISTS tags_name_unique ON tags(lower(name));
CREATE INDEX IF NOT EXISTS thread_subscriptions_target_idx ON thread_subscriptions(thread_id,uid);
CREATE INDEX IF NOT EXISTS forum_subscriptions_target_idx ON forum_subscriptions(forum_id,uid);
CREATE INDEX IF NOT EXISTS tag_subscriptions_target_idx ON tag_subscriptions(tag_id,uid);
ALTER TABLE tags ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
CREATE TABLE IF NOT EXISTS subscription_events (
 post_id bigint PRIMARY KEY REFERENCES posts(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 cursor_uid bigint NOT NULL DEFAULT 0,
 completed boolean NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS subscription_events_pending_idx ON subscription_events(post_id) WHERE NOT completed;

CREATE OR REPLACE FUNCTION queue_subscription_post() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.deleted AND NOT NEW.pending AND EXISTS(SELECT 1 FROM threads WHERE id=NEW.thread_id AND NOT deleted AND NOT pending) THEN
  INSERT INTO subscription_events(post_id) VALUES(NEW.id) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS subscription_post_event ON posts;
CREATE TRIGGER subscription_post_event AFTER INSERT OR UPDATE OF pending,deleted ON posts
 FOR EACH ROW EXECUTE FUNCTION queue_subscription_post();
CREATE OR REPLACE FUNCTION queue_subscription_thread() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.deleted AND NOT NEW.pending AND (OLD.pending OR OLD.deleted) THEN
  INSERT INTO subscription_events(post_id) SELECT id FROM posts WHERE thread_id=NEW.id AND NOT pending AND NOT deleted ON CONFLICT DO NOTHING;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS subscription_thread_event ON threads;
CREATE TRIGGER subscription_thread_event AFTER UPDATE OF pending,deleted ON threads
 FOR EACH ROW EXECUTE FUNCTION queue_subscription_thread();

CREATE TABLE IF NOT EXISTS subscription_deliveries (
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 post_id bigint NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
 PRIMARY KEY(uid,post_id)
);

CREATE OR REPLACE FUNCTION notification_group(kind text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN kind='subscription' THEN 'subscriptions'
 WHEN kind='mention' THEN 'mentions' WHEN kind IN ('reply','reply.direct') THEN 'replies'
 WHEN kind='reply.accepted' THEN 'acceptance' WHEN kind LIKE 'title.%' THEN 'titles'
 WHEN kind LIKE 'membership.%' THEN 'membership' WHEN kind LIKE 'moderation.%' THEN 'moderation'
 WHEN kind LIKE 'report.%' THEN 'reports' ELSE 'replies' END;
$$;
