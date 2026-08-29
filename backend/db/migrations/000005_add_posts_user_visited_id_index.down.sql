CREATE INDEX ix_posts_user_visited ON posts (user_id, visited_on DESC);
DROP INDEX ix_posts_user_visited_id ON posts;
