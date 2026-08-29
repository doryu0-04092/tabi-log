-- コメント。返信ツリーは作らないため親への参照は持たない。

-- name: CreateComment :execresult
INSERT INTO comments (post_id, user_id, body) VALUES (?, ?, ?);

-- name: IncrementCommentCount :exec
UPDATE posts SET comment_count = comment_count + 1 WHERE id = ?;

-- name: DecrementCommentCount :exec
UPDATE posts SET comment_count = comment_count - 1 WHERE id = ? AND comment_count > 0;

-- name: GetCommentByID :one
SELECT c.id, c.post_id, c.user_id, c.body, c.created_at,
       u.handle, u.display_name, u.bio
FROM comments c
JOIN users u ON u.id = c.user_id
WHERE c.id = ?;

-- 古い順に返す。会話は上から読むものであり、
-- フィードのように新しいものから見るのは適さない。
--
-- カーソルは id のみでよい。created_at はデータベースが挿入時に付けており
-- AUTO_INCREMENT と同じ順序になるためである。
-- 索引 ix_comments_post (post_id, created_at, id) が効く。
-- name: ListCommentsAfter :many
SELECT c.id, c.post_id, c.user_id, c.body, c.created_at,
       u.handle, u.display_name, u.bio
FROM comments c
JOIN users u ON u.id = c.user_id
WHERE c.post_id = ? AND c.id > ?
ORDER BY c.id
LIMIT ?;

-- name: DeleteCommentByID :execresult
DELETE FROM comments WHERE id = ?;
