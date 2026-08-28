-- 投稿。

-- name: CreatePost :execresult
INSERT INTO posts (user_id, body, prefecture_code, spot_name, visited_on)
VALUES (?, ?, ?, ?, ?);

-- name: GetPostByID :one
SELECT
    p.id, p.user_id, p.body, p.prefecture_code, p.spot_name, p.visited_on,
    p.like_count, p.comment_count, p.created_at, p.updated_at,
    u.handle, u.display_name, u.bio,
    pref.name AS prefecture_name, pref.name_kana AS prefecture_name_kana, pref.region
FROM posts p
JOIN users u ON u.id = p.user_id
JOIN prefectures pref ON pref.code = p.prefecture_code
WHERE p.id = ?;

-- name: UpdatePost :exec
UPDATE posts
SET body = ?, prefecture_code = ?, spot_name = ?, visited_on = ?
WHERE id = ? AND user_id = ?;

-- name: DeletePost :execresult
DELETE FROM posts
WHERE id = ? AND user_id = ?;

-- 投稿の所有者だけを取る。権限確認のために本体を全部読む必要はない。
-- name: GetPostOwner :one
SELECT user_id FROM posts WHERE id = ?;

-- 新着フィード。カーソルページネーション。
--
-- **OFFSET は使わない。** 深いページでは読み飛ばす件数だけ処理が増える。
-- 前回の最後の id より小さいものを取る形にすれば、常に一定の手数で済む。
--
-- カーソルを id だけにできるのは、**created_at をデータベースが挿入時に
-- 付けており、id（AUTO_INCREMENT）と同じ順序になる**ためである。
-- アプリケーションから created_at を指定していたら、この前提は崩れる。
--
-- 主キーの逆順走査になるため索引がそのまま効く。
-- name: ListPostsBefore :many
SELECT
    p.id, p.user_id, p.body, p.prefecture_code, p.spot_name, p.visited_on,
    p.like_count, p.comment_count, p.created_at, p.updated_at,
    u.handle, u.display_name, u.bio,
    pref.name AS prefecture_name, pref.name_kana AS prefecture_name_kana, pref.region
FROM posts p
JOIN users u ON u.id = p.user_id
JOIN prefectures pref ON pref.code = p.prefecture_code
WHERE p.id < ?
ORDER BY p.id DESC
LIMIT ?;
