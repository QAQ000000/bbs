-- 005：安全整改批次（楼层号复用 / 恢复泄露 / 永久禁言扫描 / 邮箱验证绑定）

-- 1) 楼层序号与有效帖数分离：threads.floor_seq 单调递增，
--    删除中间楼层后新回复不再复用已删除的楼层号
ALTER TABLE threads ADD COLUMN IF NOT EXISTS floor_seq int NOT NULL DEFAULT 1;
UPDATE threads t
   SET floor_seq = GREATEST(coalesce((SELECT max(p.floor) FROM posts p WHERE p.thread_id = t.id), 1), 1);

-- 2) 楼层删除批次标记：随主题删除的楼层（thread_deleted=true）在主题恢复时
--    一并恢复；此前被单独删除的楼层保持隐藏，不随主题恢复重新公开
ALTER TABLE posts ADD COLUMN IF NOT EXISTS thread_deleted boolean NOT NULL DEFAULT false;
UPDATE posts p
   SET thread_deleted = true
  FROM threads t
 WHERE t.id = p.thread_id AND t.deleted AND p.deleted;

-- 3) 公开楼层 (thread_id, floor) 唯一约束；历史重复数据存在时跳过并提示
--    （只放迁移不放 schema.sql：存量库若已有重复，schema 幂等重放不能因此失败）
DO $$
BEGIN
    CREATE UNIQUE INDEX IF NOT EXISTS posts_live_floor_uk ON posts (thread_id, floor) WHERE NOT deleted;
EXCEPTION
    WHEN unique_violation THEN
        RAISE NOTICE 'posts 存在历史重复 (thread_id, floor)，未创建唯一索引 posts_live_floor_uk';
END $$;

-- 4) 永久禁言/封禁由 'infinity' 改写为有限远期时间（pgx 无法把 infinity 扫描进
--    time.Time，导致禁言判定失效与用户列表 500）
UPDATE users SET banned_until = now() + interval '100 years'  WHERE banned_until  = 'infinity';
UPDATE users SET blocked_until = now() + interval '100 years' WHERE blocked_until = 'infinity';

-- 5) 邮箱验证令牌绑定申请时的邮箱地址：换绑邮箱后旧令牌不能验证新邮箱
ALTER TABLE email_verifications ADD COLUMN IF NOT EXISTS email text NOT NULL DEFAULT '';

-- 6) 存量回填：附件功能上线前嵌入帖子的上传记录补挂楼层
--    （否则历史图片因无记录而对游客不可见）
UPDATE uploads u
   SET post_id = x.pid
  FROM (SELECT u2.id AS uid2, min(p.id) AS pid
          FROM uploads u2
          JOIN posts p ON p.content_md LIKE '%' || u2.path || '%'
         WHERE u2.post_id IS NULL
         GROUP BY u2.id) x
 WHERE u.id = x.uid2;
