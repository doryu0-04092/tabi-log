ALTER TABLE comments
    DROP INDEX ix_comments_post,
    ADD INDEX ix_comments_post (post_id, created_at, id);
