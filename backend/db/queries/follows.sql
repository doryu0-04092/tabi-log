-- フォロー。
--
-- 主キーは (follower_id, followee_id)。追加の索引が
-- ix_follows_followee (followee_id, follower_id)。
-- 「自分がフォローしている人」と「自分をフォローしている人」の両方向を
-- それぞれ索引だけで引けるようにしてある。

-- INSERT IGNORE により、既にフォローしていても失敗しない（冪等）。
-- RowsAffected が 0 なら「既にあった」であり、通知を二重に作らずに済む。
-- name: InsertFollow :execresult
INSERT IGNORE INTO follows (follower_id, followee_id) VALUES (?, ?);

-- name: DeleteFollow :execresult
DELETE FROM follows WHERE follower_id = ? AND followee_id = ?;

-- name: CountFollowing :one
SELECT COUNT(*) FROM follows WHERE follower_id = ?;

-- name: CountFollowers :one
SELECT COUNT(*) FROM follows WHERE followee_id = ?;

-- name: IsFollowing :one
SELECT EXISTS(
    SELECT 1 FROM follows WHERE follower_id = ? AND followee_id = ?
);

-- 一覧に並んだ相手のうち、閲覧者がフォローしているのはどれか。
--
-- **一覧の1件ごとに問い合わせない。** 50件返す画面で50回の往復になる。
-- いいねの ListLikedPostIDs と同じ考え方である。
-- name: ListFollowedUserIDs :many
SELECT followee_id FROM follows
WHERE follower_id = ? AND followee_id IN (sqlc.slice('user_ids'));

-- フォロワーの一覧。
--
-- カーソルは利用者 id の昇順である。索引が (followee_id, follower_id) なので、
-- この順でのみ索引だけで続きから取れる。フォローした日時順にするなら
-- created_at を含む索引を足す必要がある。
-- name: ListFollowersAfter :many
SELECT u.id, u.handle, u.display_name, u.bio
FROM follows f
JOIN users u ON u.id = f.follower_id
WHERE f.followee_id = ? AND f.follower_id > ? AND u.deleted_at IS NULL
ORDER BY f.follower_id
LIMIT ?;

-- フォロー中の一覧。こちらは主キー (follower_id, followee_id) が効く。
-- name: ListFollowingAfter :many
SELECT u.id, u.handle, u.display_name, u.bio
FROM follows f
JOIN users u ON u.id = f.followee_id
WHERE f.follower_id = ? AND f.followee_id > ? AND u.deleted_at IS NULL
ORDER BY f.followee_id
LIMIT ?;
