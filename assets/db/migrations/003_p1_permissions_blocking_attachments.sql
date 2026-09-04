-- 003: P1 批次——权限矩阵表、封禁（禁止登录）与附件挂楼层
-- 全新库由 schema.sql 直接包含；存量库经本迁移补齐（幂等，可重复执行）。

CREATE TABLE IF NOT EXISTS role_perms (
    role_id smallint   NOT NULL,
    point   text       NOT NULL,
    allowed boolean    NOT NULL DEFAULT false,
    PRIMARY KEY (role_id, point)
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_until timestamptz;

ALTER TABLE uploads ADD COLUMN IF NOT EXISTS post_id bigint;
CREATE INDEX IF NOT EXISTS uploads_post_idx ON uploads (post_id) WHERE post_id IS NOT NULL;
