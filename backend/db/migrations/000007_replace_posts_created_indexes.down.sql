CREATE INDEX ix_posts_prefecture_created ON posts (prefecture_code, created_at DESC);
DROP INDEX ix_posts_prefecture_id ON posts;

CREATE INDEX ix_posts_user_created ON posts (user_id, created_at DESC);
