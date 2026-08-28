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
