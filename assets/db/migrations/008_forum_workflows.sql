-- Forum workflow metadata and durable, deduplicated in-app notifications.
ALTER TABLE posts ADD COLUMN IF NOT EXISTS reply_to_post_id bigint REFERENCES posts(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS posts_reply_to_idx ON posts(reply_to_post_id) WHERE reply_to_post_id IS NOT NULL;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS moderation_status text NOT NULL DEFAULT '';
ALTER TABLE posts ADD COLUMN IF NOT EXISTS moderation_note text NOT NULL DEFAULT '';
ALTER TABLE drafts ADD COLUMN IF NOT EXISTS subject text NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS event_key text;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'content';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS payload jsonb NOT NULL DEFAULT '{}';
CREATE UNIQUE INDEX IF NOT EXISTS notifications_event_key ON notifications(uid,event_key) WHERE event_key IS NOT NULL;
CREATE TABLE IF NOT EXISTS notification_preferences (
 uid bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 body jsonb NOT NULL DEFAULT '{}'
);
CREATE OR REPLACE FUNCTION notification_group(kind text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN kind='mention' THEN 'mentions' WHEN kind IN ('reply','reply.direct') THEN 'replies'
 WHEN kind='reply.accepted' THEN 'acceptance' WHEN kind LIKE 'title.%' THEN 'titles'
 WHEN kind LIKE 'membership.%' THEN 'membership' WHEN kind LIKE 'moderation.%' THEN 'moderation'
 WHEN kind LIKE 'report.%' THEN 'reports' ELSE 'replies' END;
$$;
CREATE OR REPLACE FUNCTION forum_notify(recipient bigint, sender bigint, sender_name text, kind text,
 tid bigint, pid bigint, summary text, event text, visibility text DEFAULT 'content', data jsonb DEFAULT '{}')
 RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE result bigint;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM users WHERE id=recipient) THEN RETURN 0; END IF;
 IF EXISTS(SELECT 1 FROM notification_preferences WHERE uid=recipient AND body->>notification_group(kind)='false') THEN RETURN 0; END IF;
 INSERT INTO notifications(uid,from_uid,from_name,type,thread_id,post_id,excerpt,event_key,scope,payload)
 VALUES(recipient,sender,sender_name,kind,tid,pid,summary,event,visibility,data)
 ON CONFLICT(uid,event_key) WHERE event_key IS NOT NULL DO NOTHING RETURNING id INTO result;
 RETURN coalesce(result,0);
END $$;
-- Audit records provide stable identities and share the originating transaction.
CREATE OR REPLACE FUNCTION notify_title_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE label text;
BEGIN
 IF NEW.action NOT IN ('grant','revoke') OR NEW.user_id IS NULL THEN RETURN NULL; END IF;
 SELECT body->>'name' INTO label FROM titles WHERE id=NEW.title_id;
 PERFORM forum_notify(NEW.user_id,coalesce(NEW.actor_id,0),'系统',
 CASE NEW.action WHEN 'grant' THEN 'title.granted' ELSE 'title.revoked' END,0,0,
 CASE NEW.action WHEN 'grant' THEN '获得称号：' ELSE '称号已撤销：' END||coalesce(label,''),
 'title-log:'||NEW.id,'account',jsonb_build_object('titleId',NEW.title_id::text));
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS notify_title_change ON title_logs;
CREATE TRIGGER notify_title_change AFTER INSERT ON title_logs FOR EACH ROW EXECUTE FUNCTION notify_title_change();
CREATE OR REPLACE FUNCTION notify_member_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.action='level.upgrade' AND NEW.user_id IS NOT NULL THEN
  PERFORM forum_notify(NEW.user_id,0,'系统','membership.upgraded',0,0,'会员等级已升级',
   'member-change:'||NEW.id,'account',jsonb_build_object('levelId',NEW.detail->>'to'));
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS notify_member_change ON member_changes;
CREATE TRIGGER notify_member_change AFTER INSERT ON member_changes FOR EACH ROW EXECUTE FUNCTION notify_member_change();
CREATE OR REPLACE FUNCTION notify_accepted_reply() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE recipient bigint;
BEGIN
 SELECT author_id INTO recipient FROM posts WHERE id=NEW.post_id;
 IF recipient IS NOT NULL AND recipient<>NEW.actor_id AND NEW.accepted THEN
  PERFORM forum_notify(recipient,NEW.actor_id,(SELECT username FROM users WHERE id=NEW.actor_id),
   'reply.accepted',NEW.thread_id,NEW.post_id,'你的回复已被主题作者采纳',
   'accepted:'||NEW.post_id,'content');
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS notify_accepted_reply ON acceptance_logs;
CREATE TRIGGER notify_accepted_reply AFTER INSERT ON acceptance_logs FOR EACH ROW EXECUTE FUNCTION notify_accepted_reply();
CREATE OR REPLACE FUNCTION notify_moderation_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.moderation_status IN ('approved','rejected') AND OLD.moderation_status IS DISTINCT FROM NEW.moderation_status THEN
  PERFORM forum_notify(NEW.author_id,0,'系统','moderation.'||NEW.moderation_status,NEW.thread_id,NEW.id,
   CASE NEW.moderation_status WHEN 'approved' THEN '你的内容已通过审核' ELSE '你的内容未通过审核：'||NEW.moderation_note END,
   'moderation:'||NEW.id||':'||NEW.version||':'||NEW.moderation_status,'account',
   jsonb_build_object('status',NEW.moderation_status));
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS notify_moderation_result ON posts;
CREATE TRIGGER notify_moderation_result AFTER UPDATE OF moderation_status ON posts FOR EACH ROW EXECUTE FUNCTION notify_moderation_result();
CREATE OR REPLACE FUNCTION notify_report_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>OLD.status AND NEW.status IN ('resolved','dismissed') THEN
  PERFORM forum_notify(NEW.reporter,coalesce(NEW.handled_by,0),'系统','report.'||NEW.status,0,0,
   CASE NEW.status WHEN 'resolved' THEN '你的举报已处理' ELSE '你的举报已驳回' END,
   'report:'||NEW.id||':'||NEW.status,'account',jsonb_build_object('reportId',NEW.id::text));
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS notify_report_result ON reports;
CREATE TRIGGER notify_report_result AFTER UPDATE OF status ON reports FOR EACH ROW EXECUTE FUNCTION notify_report_result();
