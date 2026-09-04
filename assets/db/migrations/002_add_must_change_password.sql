-- 002: -seed 初始账号强制改密标志（ROADMAP 5.1）
-- 全新库由 schema.sql 直接包含本列；存量库经本迁移补齐（幂等，可重复执行）。
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password boolean NOT NULL DEFAULT false;
