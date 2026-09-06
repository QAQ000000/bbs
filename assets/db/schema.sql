-- forum 数据库初始 schema（PostgreSQL 18）
-- 全新设计，不复刻 Discuz 表结构。

-- users 用户：登录、用户组（0=会员 1=管理员 2=版主）、信任等级与阅读统计
CREATE TABLE IF NOT EXISTS users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      text        NOT NULL,
    password_hash text        NOT NULL,
    email         text        NOT NULL DEFAULT '',
    group_id      smallint    NOT NULL DEFAULT 0,   -- 0=会员 1=管理员
    post_count    bigint      NOT NULL DEFAULT 0,
    signature     text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    banned_until  timestamptz,                      -- 禁言截止（NULL=未禁言）
    ban_reason    text        NOT NULL DEFAULT '',
    must_change_password boolean NOT NULL DEFAULT false  -- -seed 初始账号首次登录强制改密
);
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_idx ON users (lower(username));

-- categories 版块顶部分类
CREATE TABLE IF NOT EXISTS categories (
    id           smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         text NOT NULL,
    displayorder int  NOT NULL DEFAULT 0
);

-- forums 版块：含为列表页反范式化的统计与最后发表信息；
-- moderators 仅为后台表单展示用 CSV，权威数据在 forum_moderators
CREATE TABLE IF NOT EXISTS forums (
    id                int GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    category_id       smallint NOT NULL REFERENCES categories(id),
    name              text     NOT NULL,
    description       text     NOT NULL DEFAULT '',
    displayorder      int      NOT NULL DEFAULT 0,
    thread_count      bigint   NOT NULL DEFAULT 0,
    post_count        bigint   NOT NULL DEFAULT 0,
    today_count       int      NOT NULL DEFAULT 0,
    today_date        date,
    last_post_at      timestamptz,
    last_post_uid     bigint,
    last_post_author  text,
    last_thread_id    bigint,
    last_thread_title text
);
CREATE INDEX IF NOT EXISTS forums_cat_idx ON forums (category_id, displayorder);

-- threads 主题：pending=true 表示待审核（公开不可见）；deleted=true 进入回收站
CREATE TABLE IF NOT EXISTS threads (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    forum_id      int        NOT NULL REFERENCES forums(id),
    author_id     bigint     NOT NULL REFERENCES users(id),
    title         text       NOT NULL,
    sticky        smallint   NOT NULL DEFAULT 0,   -- 0=普通 1..3=置顶级别
    digest        boolean    NOT NULL DEFAULT false,
    closed        boolean    NOT NULL DEFAULT false,
    post_count    int        NOT NULL DEFAULT 0,   -- 含首楼
    view_count    bigint     NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_post_at  timestamptz NOT NULL DEFAULT now(),
    last_post_uid bigint,
    first_post_id bigint,
    deleted       boolean    NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS threads_forum_sticky_idx
    ON threads (forum_id, last_post_at DESC) WHERE NOT deleted AND sticky = 0;
CREATE INDEX IF NOT EXISTS threads_sticky_idx
    ON threads (forum_id, sticky DESC) WHERE NOT deleted AND sticky > 0;

-- posts 楼层：content_md 原文 + content_html 渲染缓存（写时渲染）；version 用于编辑冲突检测
CREATE TABLE IF NOT EXISTS posts (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    thread_id    bigint     NOT NULL REFERENCES threads(id),
    author_id    bigint     NOT NULL REFERENCES users(id),
    floor        int        NOT NULL,
    content_md   text       NOT NULL,
    content_html text       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    edited_at    timestamptz,
    deleted      boolean    NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS posts_thread_floor_idx
    ON posts (thread_id, floor) WHERE NOT deleted;

-- sessions 服务端会话：token 为 HttpOnly Cookie 值，csrf 绑定会话防跨站请求伪造
CREATE TABLE IF NOT EXISTS sessions (
    token      text        PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expires_at);

-- ---- 后台（阶段一）----

-- settings 站点设置 KV（站名/每页条数/注册/审核/上传/关站等，后台管理）
CREATE TABLE IF NOT EXISTS settings (
    key   text PRIMARY KEY,
    value text NOT NULL DEFAULT ''
);

-- admin_logs 后台操作审计日志
CREATE TABLE IF NOT EXISTS admin_logs (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uid        bigint      NOT NULL,
    username   text        NOT NULL,
    action     text        NOT NULL,
    detail     text        NOT NULL DEFAULT '',
    ip         text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS admin_logs_created_idx ON admin_logs (created_at DESC);

-- 存量库增量列（新库由上方 CREATE 直接包含）
ALTER TABLE users ADD COLUMN IF NOT EXISTS banned_until timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS ban_reason text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_until timestamptz; -- 封禁（禁止登录）截止；与禁言（banned_until）分离

-- ---- 后台（阶段二）----

-- announcements 首页公告
CREATE TABLE IF NOT EXISTS announcements (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uid        bigint      NOT NULL DEFAULT 0,
    author     text        NOT NULL DEFAULT '',
    content    text        NOT NULL,
    enabled    boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- censor_words 敏感词：发帖时替换为 replacement
CREATE TABLE IF NOT EXISTS censor_words (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    word        text        NOT NULL,
    replacement text        NOT NULL DEFAULT '*',
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT censor_words_word_key UNIQUE (word)
);

-- ---- 后台（阶段三）----
ALTER TABLE threads ADD COLUMN IF NOT EXISTS pending boolean NOT NULL DEFAULT false;
ALTER TABLE posts   ADD COLUMN IF NOT EXISTS pending boolean NOT NULL DEFAULT false;
ALTER TABLE forums  ADD COLUMN IF NOT EXISTS moderators text NOT NULL DEFAULT '';

-- forum_moderators 版主管辖（forum_id, user_id）；改名不再丢权
CREATE TABLE IF NOT EXISTS forum_moderators (
    forum_id int    NOT NULL REFERENCES forums(id) ON DELETE CASCADE,
    user_id  bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (forum_id, user_id)
);
CREATE INDEX IF NOT EXISTS forum_moderators_user_idx ON forum_moderators (user_id);

-- 存量 CSV → 关系表（trim + 大小写不敏感匹配用户名）
INSERT INTO forum_moderators (forum_id, user_id)
SELECT DISTINCT f.id, u.id
FROM forums f
CROSS JOIN LATERAL unnest(string_to_array(f.moderators, ',')) AS m(name)
JOIN users u ON lower(u.username) = lower(btrim(m.name))
WHERE coalesce(f.moderators, '') <> '' AND btrim(m.name) <> ''
ON CONFLICT DO NOTHING;

-- ---- 互动与体验（Discourse 借鉴批次）----

-- 点赞等动作（Discourse post_actions 模型：一张表多动作，计数冗余回写 posts）
-- post_actions 点赞等动作（pid+uid+action 主键防重），计数冗余回写 posts.like_count
CREATE TABLE IF NOT EXISTS post_actions (
    pid        bigint      NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    uid        bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action     smallint    NOT NULL DEFAULT 1,   -- 1=点赞
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pid, uid, action)
);

-- 服务端草稿（Discourse drafts 模型：按上下文保存，发帖成功即删）
-- drafts 服务端草稿（按 user+context 存取，发帖成功即删）
CREATE TABLE IF NOT EXISTS drafts (
    user_id    bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    context    text        NOT NULL,             -- new:<fid> / reply:<tid> / edit:<pid>
    content    text        NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, context)
);

ALTER TABLE posts   ADD COLUMN IF NOT EXISTS like_count     int NOT NULL DEFAULT 0;
ALTER TABLE posts   ADD COLUMN IF NOT EXISTS version        int NOT NULL DEFAULT 1;
ALTER TABLE posts   ADD COLUMN IF NOT EXISTS pending_reason text NOT NULL DEFAULT '';
ALTER TABLE threads ADD COLUMN IF NOT EXISTS pending_reason text NOT NULL DEFAULT '';

-- ---- 搜索与信任等级（Discourse 借鉴批次二）----

-- 全文搜索：tsvector 由应用层分词后写入（中文 bigram，simple 配置）
ALTER TABLE posts ADD COLUMN IF NOT EXISTS search_data tsvector;
CREATE INDEX IF NOT EXISTS posts_search_idx ON posts USING GIN (search_data);

-- 阅读追踪（Discourse post_timings/topic_users 的简化版）
-- thread_reads 阅读进度（用户读到某主题第几楼），支撑 posts_read 统计与信任等级升级
CREATE TABLE IF NOT EXISTS thread_reads (
    user_id    bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id  bigint      NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    last_floor int         NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, thread_id)
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS posts_read      bigint   NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS days_visited    int      NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_visit_date date;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified  boolean  NOT NULL DEFAULT false; -- 邮箱验证开关开启时注册用户需验证

-- email_verifications 邮箱验证令牌（库中存哈希，24h 有效，一用户一令牌）
CREATE TABLE IF NOT EXISTS email_verifications (
    uid        bigint      PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- ---- 上传与通知 ----

-- uploads 上传记录（文件落盘 data/uploads/，仓库不含）
CREATE TABLE IF NOT EXISTS uploads (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uid        bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       text        NOT NULL,           -- 原始文件名
    path       text        NOT NULL,           -- 站点内路径 /uploads/...
    size       bigint      NOT NULL,
    mime       text        NOT NULL,
    post_id    bigint,                         -- 关联楼层（发帖时按内容回填）
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE uploads ADD COLUMN IF NOT EXISTS post_id bigint; -- 存量库补列（新库由 CREATE TABLE 直接包含）
CREATE INDEX IF NOT EXISTS uploads_post_idx ON uploads (post_id) WHERE post_id IS NOT NULL;

-- notifications 提及等站内通知（read=false 未读）
CREATE TABLE IF NOT EXISTS notifications (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uid        bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_uid   bigint      NOT NULL,
    from_name  text        NOT NULL,
    type       text        NOT NULL DEFAULT 'mention',
    thread_id  bigint      NOT NULL,
    post_id    bigint      NOT NULL,
    excerpt    text        NOT NULL DEFAULT '',
    read       boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notifications_uid_idx ON notifications (uid, read, id DESC);

-- ---- 安全与治理加固 ----

-- 密码重置令牌（库中只存哈希，原始 token 仅出现在邮件链接里）
CREATE TABLE IF NOT EXISTS password_resets (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uid        bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used       boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 邮箱部分唯一索引（空串不限；注册/改邮箱冲突由索引兜底）
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_idx ON users (email) WHERE email <> '';

-- reports 举报（ROADMAP 阶段三）：会员举报楼层，staff 在审核队列处理
CREATE TABLE IF NOT EXISTS reports (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    post_id    bigint      NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    reporter   bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason     text        NOT NULL DEFAULT '',
    status     text        NOT NULL DEFAULT 'open',   -- open / resolved / dismissed
    handled_by bigint      NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    handled_at timestamptz
);
-- 同一用户对同一楼层只允许一条未处理举报
CREATE UNIQUE INDEX IF NOT EXISTS reports_open_unique_idx ON reports (post_id, reporter) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS reports_status_idx ON reports (status, id DESC);

-- 迁移版本基线：schema.sql 整体幂等重放；后续破坏性变更以递增 version 的
-- 编号迁移文件处理并在本表登记（当前全部历史合并为基线 1）
-- role_perms 角色权限矩阵：perm.Allowed 的运行时数据源，后台矩阵页可编辑
CREATE TABLE IF NOT EXISTS role_perms (
    role_id smallint   NOT NULL,              -- 0=会员 1=管理员 2=版主
    point   text       NOT NULL,              -- 权限点（perm.Point）
    allowed boolean    NOT NULL DEFAULT false,
    PRIMARY KEY (role_id, point)
);

-- thread_favorites 收藏/订阅（个人页收藏列表 + 未读新回复提醒）
CREATE TABLE IF NOT EXISTS thread_favorites (
    user_id    bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id  bigint      NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, thread_id)
);
CREATE INDEX IF NOT EXISTS thread_favorites_user_idx ON thread_favorites (user_id, thread_id DESC);

-- post_edits 编辑历史（每次编辑保存改前快照，作者与版主可查）
CREATE TABLE IF NOT EXISTS post_edits (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    post_id    bigint      NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    editor_id  bigint      NOT NULL,
    content_md text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS post_edits_post_idx ON post_edits (post_id, id DESC);

ALTER TABLE posts ADD COLUMN IF NOT EXISTS ip text NOT NULL DEFAULT ''; -- 发布时记录来源 IP（隐私政策声明，仅管理员可见掩码）

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    int         PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO schema_migrations (version) VALUES (1) ON CONFLICT DO NOTHING;

-- ---- 安全整改批次（迁移 005）----
ALTER TABLE threads ADD COLUMN IF NOT EXISTS floor_seq int NOT NULL DEFAULT 1;            -- 单调楼层序号（与有效帖数分离，删楼不复用楼层号）
ALTER TABLE posts   ADD COLUMN IF NOT EXISTS thread_deleted boolean NOT NULL DEFAULT false; -- 随主题删除的楼层标记（恢复主题时只恢复这批）
ALTER TABLE email_verifications ADD COLUMN IF NOT EXISTS email text NOT NULL DEFAULT '';    -- 验证令牌绑定的申请邮箱（换绑后旧令牌失效）

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
