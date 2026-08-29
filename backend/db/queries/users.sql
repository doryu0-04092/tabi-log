-- 利用者。

-- name: CreateUser :execresult
INSERT INTO users (handle, email, password_hash, display_name)
VALUES (?, ?, ?, ?);

-- ログイン用。退会済みは対象外にする。
-- name: GetUserByEmail :one
SELECT id, handle, email, password_hash, display_name, bio, avatar_media_id
FROM users
WHERE email = ? AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT id, handle, email, password_hash, display_name, bio, avatar_media_id
FROM users
WHERE id = ? AND deleted_at IS NULL;

-- ハンドルで引く。URL に使う識別子であり、プロフィールの入口になる。
-- name: GetUserByHandle :one
SELECT id, handle, email, password_hash, display_name, bio, avatar_media_id
FROM users
WHERE handle = ? AND deleted_at IS NULL;

-- name: CountPostsByUser :one
SELECT COUNT(*) FROM posts WHERE user_id = ?;

-- 投稿した都道府県の種類数。制覇率の分子になる。
-- 索引 ix_posts_user_prefecture (user_id, prefecture_code) が効く。
-- name: CountVisitedPrefectures :one
SELECT COUNT(DISTINCT prefecture_code) FROM posts WHERE user_id = ?;
