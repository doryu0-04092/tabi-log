-- いいね。
--
-- 主キーの列順が (user_id, post_id) であることに意味がある。
-- フィードでは「表示する20件のうち自分がいいね済みなのはどれか」を
-- WHERE user_id = ? AND post_id IN (...) で求めるため、この順でないと効かない。

-- 冪等にする。既にあれば何もしない。
-- **更新件数で「新しく入ったか」を判定し、カウンタの増減に使う。**
-- 事前に SELECT で確認すると、確認と INSERT の間に割り込まれる余地が残る。
-- name: InsertLike :execresult
INSERT IGNORE INTO likes (user_id, post_id) VALUES (?, ?);

-- name: DeleteLike :execresult
DELETE FROM likes WHERE user_id = ? AND post_id = ?;

-- name: IncrementLikeCount :exec
UPDATE posts SET like_count = like_count + 1 WHERE id = ?;

-- カウンタが負にならないようにする。
-- 万一ずれても 0 未満にはしない（UNSIGNED なので負にすると桁あふれする）。
-- name: DecrementLikeCount :exec
UPDATE posts SET like_count = like_count - 1 WHERE id = ? AND like_count > 0;

-- フィード用。**投稿ごとに問い合わせない。**
-- name: ListLikedPostIDs :many
SELECT post_id FROM likes
WHERE user_id = ? AND post_id IN (sqlc.slice('post_ids'));

-- name: IsPostLiked :one
SELECT EXISTS(SELECT 1 FROM likes WHERE user_id = ? AND post_id = ?) AS liked;
