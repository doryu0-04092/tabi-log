-- 旅行履歴（訪問日順）のための索引。
--
-- 一覧は WHERE user_id = ? AND (visited_on, id) < (?, ?)
-- ORDER BY visited_on DESC, id DESC で引く。
-- **訪問日は重複する**ため、カーソルは日付だけでは足りず (訪問日, ID) の組になる。
--
-- 既存の ix_posts_user_visited (user_id, visited_on DESC) では、
-- InnoDB が末尾に付ける主キーが昇順であり、id の降順を索引で解決できない。
-- 000003・000004 と同じ形の修正である。
CREATE INDEX ix_posts_user_visited_id ON posts (user_id, visited_on DESC, id DESC);
DROP INDEX ix_posts_user_visited ON posts;
