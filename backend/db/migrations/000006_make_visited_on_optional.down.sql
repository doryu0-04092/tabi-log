-- 戻す前に、訪問日の無い投稿を投稿日で埋める必要がある。
-- NULL のまま NOT NULL へ戻すと失敗する。
UPDATE posts SET visited_on = DATE(created_at) WHERE visited_on IS NULL;
ALTER TABLE posts MODIFY COLUMN visited_on DATE NOT NULL;
