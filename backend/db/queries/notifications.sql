-- 通知。
--
-- **契機となる操作（いいね・コメント・フォロー）と同一トランザクションで
-- INSERT する。** キューや Outbox は使わない。「いいねは記録されたが
-- 通知が消えた」状態を作らないための最も単純な方法である。

-- name: CreateNotification :exec
INSERT INTO notifications (user_id, actor_id, type, post_id, comment_id)
VALUES (?, ?, ?, ?, ?);

-- いいねを取り消したときに、対応する通知も消す。
-- 残すと「いいねされた」通知だけが宙に浮く。
-- name: DeleteLikeNotification :exec
DELETE FROM notifications
WHERE user_id = ? AND actor_id = ? AND type = 'like' AND post_id = ?;
