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
-- 索引 ix_comments_post (post_id, id) が効く。
--
-- **2列目が created_at だった頃は効いていなかった。** post_id で絞った
-- あとの並びが created_at → id になり、id の範囲を索引で辿れないため、
-- MySQL は PRIMARY を id > ? で範囲走査して post_id を捨てる計画を選ぶ。
-- 並べ替えは出ないので Extra だけを見ていると気づけない（000009 で置換）。
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
