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
    ban_reason    text        NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_idx ON users (lower(username));

-- categories 版块顶部分类
CREATE TABLE IF NOT EXISTS categories (
    id           smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         text NOT NULL,
    displayorder int  NOT NULL DEFAULT 0
);

-- forums 版块：含为列表页反范式化的统计与最后发表信息；moderators 为版主用户名 CSV
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

ALTER TABLE users ADD COLUMN IF NOT EXISTS trust_level     smallint NOT NULL DEFAULT 0; -- 0=新用户 1=正式成员 2=资深成员
ALTER TABLE users ADD COLUMN IF NOT EXISTS posts_read      bigint   NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS days_visited    int      NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_visit_date date;

-- ---- 上传与通知 ----

-- uploads 上传记录（文件落盘 data/uploads/，仓库不含）
CREATE TABLE IF NOT EXISTS uploads (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uid        bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       text        NOT NULL,           -- 原始文件名
    path       text        NOT NULL,           -- 站点内路径 /uploads/...
    size       bigint      NOT NULL,
    mime       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

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

-- 迁移版本基线：schema.sql 整体幂等重放；后续破坏性变更以递增 version 的
-- 编号迁移文件处理并在本表登记（当前全部历史合并为基线 1）
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    int         PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO schema_migrations (version) VALUES (1) ON CONFLICT DO NOTHING;
