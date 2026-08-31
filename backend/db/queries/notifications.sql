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

-- name: DeleteFollowNotification :exec
DELETE FROM notifications
WHERE user_id = ? AND actor_id = ? AND type = 'follow';

-- 通知の一覧。新しい順。カーソルは id のみ（フィードと同じ考え方）。
-- **索引を明示している。** notifications には user_id で始まる索引が
-- 2本ある（ix_notifications_user_id と、未読数のための
-- ix_notifications_user_read）。絞り込みの効き目は同じに見えるため、
-- **オプティマイザが並べ替えを解決できない側（user_read）を選ぶ。**
-- 実測（2026-08-29、通知2万件）では指定なしで Using filesort が出た。
-- ix_notifications_user_read は未読数の問い合わせに要るので落とせない。
-- name: ListNotificationsBefore :many
SELECT
    n.id, n.type, n.post_id, n.comment_id, n.read_at, n.created_at,
    u.id AS actor_id, u.handle, u.display_name, u.bio,
    c.body AS comment_body
FROM notifications n FORCE INDEX (ix_notifications_user_id)
JOIN users u ON u.id = n.actor_id
LEFT JOIN comments c ON c.id = n.comment_id
WHERE n.user_id = ? AND n.id < ?
ORDER BY n.id DESC
LIMIT ?;

-- 未読の件数。見出しの数のためだけに引く軽い問い合わせ。
-- 索引 ix_notifications_user_read (user_id, read_at) が効く。
-- name: CountUnreadNotifications :one
SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL;

-- **user_id を条件に入れているのは権限の担保である。**
-- 他人あての通知を id だけで既読にできてはいけない。
-- name: MarkNotificationRead :execresult
UPDATE notifications SET read_at = ? WHERE id = ? AND user_id = ? AND read_at IS NULL;

-- 既読のものは触らない。既読の時刻を上書きしないためである。
-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL;

-- 既読にできるかの確認に使う。存在しない場合と他人あての場合を区別しない。
-- name: NotificationExists :one
SELECT EXISTS(SELECT 1 FROM notifications WHERE id = ? AND user_id = ?);

-- 退会したときに、その人が関わる通知を両方向とも消す。
--
-- **外部キーの ON DELETE CASCADE は効かない。** 退会は users 行を
-- 残す論理削除であり、行が消えないため連鎖しない。
--
-- actor_id 側を消さないと、相手の一覧に「退会したユーザーがいいね
-- しました」が残り続ける。リンク先は 404 になる。
-- name: DeleteNotificationsInvolvingUser :exec
DELETE FROM notifications
WHERE user_id = ? OR actor_id = ?;
