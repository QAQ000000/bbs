-- Public feeds and bounded date-range aggregates.
CREATE INDEX IF NOT EXISTS threads_public_latest_idx
 ON threads(last_post_at DESC,id DESC) WHERE NOT deleted AND NOT pending;
CREATE INDEX IF NOT EXISTS threads_public_new_idx
 ON threads(forum_id,created_at DESC,id DESC) WHERE NOT deleted AND NOT pending AND sticky=0;
CREATE INDEX IF NOT EXISTS threads_public_author_idx
 ON threads(author_id,last_post_at DESC,id DESC) WHERE NOT deleted AND NOT pending;
CREATE INDEX IF NOT EXISTS posts_public_created_idx
 ON posts(created_at,thread_id) WHERE NOT deleted AND NOT pending;
