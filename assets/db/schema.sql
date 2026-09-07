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

-- Durable email outbox. Tokens are encrypted with an application-owned key.
CREATE TABLE IF NOT EXISTS email_jobs (
 id bigserial PRIMARY KEY,
 uid bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('password_reset','email_verify','mention','reply','reply.direct','subscription')),
 template_version integer NOT NULL DEFAULT 1 CHECK(template_version=1),
 recipient text NOT NULL,
 post_id bigint REFERENCES posts(id) ON DELETE CASCADE,
 token_hash text NOT NULL DEFAULT '',
 sealed_token text NOT NULL DEFAULT '',
 dedup_key text NOT NULL UNIQUE,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','dead','cancelled')),
 priority integer NOT NULL DEFAULT 0,
 attempts integer NOT NULL DEFAULT 0,
 retries integer NOT NULL DEFAULT 0,
 version bigint NOT NULL DEFAULT 1,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 expires_at timestamptz NOT NULL,
 sent_at timestamptz,
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS email_jobs_pending_idx ON email_jobs(priority DESC,next_attempt_at,id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS email_jobs_lease_idx ON email_jobs(lease_until) WHERE status='sending';

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

CREATE TABLE IF NOT EXISTS user_mfa (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret_cipher text NOT NULL, enabled boolean NOT NULL DEFAULT false,
 enabled_at timestamptz, last_step bigint NOT NULL DEFAULT -1,
 recovery_hashes text[] NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mfa_challenges (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 password_hash text NOT NULL, expires_at timestamptz NOT NULL, used_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS mfa_challenges_expiry_idx ON mfa_challenges(expires_at);

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

-- Transactional MFA enrollment and browser-bound login challenges.
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS version text NOT NULL DEFAULT '';
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS setup_session bigint;
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS setup_password text;
ALTER TABLE user_mfa ADD COLUMN IF NOT EXISTS setup_expires timestamptz;
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS token_hash text;
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS csrf_hash text NOT NULL DEFAULT '';
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS mfa_version text NOT NULL DEFAULT '';
ALTER TABLE mfa_challenges ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS mfa_challenges_token_idx ON mfa_challenges(token_hash);
CREATE TABLE IF NOT EXISTS mfa_attempts (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 scope text NOT NULL,
 window_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 1,
 PRIMARY KEY(user_id,scope)
);
-- Legacy challenges cannot be completed; active secrets remain protected.
UPDATE mfa_challenges SET used_at=now() WHERE token_hash IS NULL AND used_at IS NULL;
UPDATE user_mfa SET recovery_hashes='{}' WHERE EXISTS (
 SELECT 1 FROM unnest(recovery_hashes) h WHERE length(h)<>64
);
