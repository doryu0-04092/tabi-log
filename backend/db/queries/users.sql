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

-- name: UpdateProfile :exec
UPDATE users SET display_name = ?, bio = ? WHERE id = ? AND deleted_at IS NULL;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = ? WHERE id = ? AND deleted_at IS NULL;

-- 退会。**物理削除しない。** ハンドルは解放せず保持する。
-- 解放すると別人が同じハンドルを取れてしまい、過去のリンクの指す先が変わる。
--
-- メールアドレスは復元不能な値に置き換える。UNIQUE 制約があるため、
-- 元のアドレスで再登録できるようになる。
-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = ?,
    email = ?,
    display_name = '退会したユーザー',
    bio = NULL,
    avatar_media_id = NULL,
    password_hash = ?
WHERE id = ? AND deleted_at IS NULL;

-- 退会時に消す対象。S3 のオブジェクトは外部キーの連鎖では消えないため、
-- 鍵を集めてから削除する。
-- name: ListS3KeysByUser :many
SELECT m.s3_key FROM media m WHERE m.user_id = ?
UNION
SELECT v.s3_key FROM media_variants v
JOIN media m2 ON m2.id = v.media_id
WHERE m2.user_id = ?;

-- name: DeletePostsByUser :exec
DELETE FROM posts WHERE user_id = ?;

-- name: DeleteMediaByUser :exec
DELETE FROM media WHERE user_id = ?;

-- **消す前にカウンタ列を減らす。**
-- posts.comment_count / like_count は行を消しても自動では減らない。
-- 減らさないと、退会者がコメントしていた他人の投稿の件数がずれたまま残る。
--
-- 自分の投稿は先に消してある（連鎖でその投稿へのコメントも消える）ため、
-- ここで残っているのは**他人の投稿に付けたもの**だけである。
--
-- **UPDATE ... JOIN では書けない。** sqlc が posts.user_id と
-- comments.user_id を区別できず「曖昧」と判断する（実測）。相関副問い合わせで書く。
--
-- **副問い合わせの中でも表の別名で修飾する。** 修飾しないと、
-- 外側の posts.user_id と同名になり、やはり曖昧だと判断される。
--
-- GREATEST と CAST を使うのは、UNSIGNED 列が負の値で失敗するためである。
-- name: DecrementCommentCountsForUser :exec
UPDATE posts
SET comment_count = GREATEST(
    CAST(comment_count AS SIGNED)
    - (SELECT COUNT(*) FROM comments c WHERE c.post_id = posts.id AND c.user_id = ?),
    0)
WHERE id IN (SELECT c2.post_id FROM comments c2 WHERE c2.user_id = ?);

-- いいねは (user_id, post_id) が主キーなので1投稿につき1件である。
-- name: DecrementLikeCountsForUser :exec
UPDATE posts
SET like_count = GREATEST(CAST(like_count AS SIGNED) - 1, 0)
WHERE id IN (SELECT l.post_id FROM likes l WHERE l.user_id = ?);

-- name: DeleteCommentsByUser :exec
DELETE FROM comments WHERE user_id = ?;

-- name: DeleteLikesByUser :exec
DELETE FROM likes WHERE user_id = ?;

-- name: DeleteFollowsByUser :exec
DELETE FROM follows WHERE follower_id = ? OR followee_id = ?;
