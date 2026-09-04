-- 004: P2 批次——收藏/订阅、编辑历史与楼层 IP（对齐隐私政策）
-- 全新库由 schema.sql 直接包含；存量库经本迁移补齐（幂等，可重复执行）。

CREATE TABLE IF NOT EXISTS thread_favorites (
    user_id    bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id  bigint      NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, thread_id)
);
CREATE INDEX IF NOT EXISTS thread_favorites_user_idx ON thread_favorites (user_id, thread_id DESC);

CREATE TABLE IF NOT EXISTS post_edits (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    post_id    bigint      NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    editor_id  bigint      NOT NULL,
    content_md text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS post_edits_post_idx ON post_edits (post_id, id DESC);

ALTER TABLE posts ADD COLUMN IF NOT EXISTS ip text NOT NULL DEFAULT '';
