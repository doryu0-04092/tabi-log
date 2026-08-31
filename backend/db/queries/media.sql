-- 画像。status が「アップロードされる予定」を表す状態機械になっている。

-- 署名付きURLを発行する時点で pending として先に記録する（write-ahead）。
-- これが無いと、送信後に投稿が確定されなかったオブジェクトを後から特定できない。
-- name: CreatePendingMedia :execresult
INSERT INTO media (user_id, s3_key, status)
VALUES (?, ?, 'pending');

-- name: GetMediaByID :one
SELECT id, user_id, post_id, s3_key, mime, width, height, bytes, sort_order, status
FROM media
WHERE id = ?;

-- name: GetMediaByS3Key :one
SELECT id, user_id, post_id, s3_key, mime, width, height, bytes, sort_order, status
FROM media
WHERE s3_key = ?;

-- 投稿に紐づける。
--
-- 条件が要点である。**自分のもので、処理が完了しており、まだどの投稿にも
-- 属していない**画像だけを紐づける。条件に合わなければ更新件数が 0 になり、
-- 呼び出し側がそれを検出する。SELECT で確認してから UPDATE すると、
-- その間に別のリクエストが同じ画像を使う余地が残る。
-- name: AttachMediaToPost :execresult
UPDATE media
SET post_id = ?, sort_order = ?
WHERE id = ?
  AND user_id = ?
  AND post_id IS NULL
  AND status = 'processed';

-- name: ListMediaByPostID :many
SELECT id, post_id, width, height, s3_key
FROM media
WHERE post_id = ?
ORDER BY sort_order, id;

-- 画像処理の完了。
-- name: MarkMediaProcessed :exec
UPDATE media
SET status = 'processed', mime = ?, width = ?, height = ?, bytes = ?
WHERE id = ?;

-- name: MarkMediaFailed :exec
UPDATE media
SET status = 'failed'
WHERE id = ?;

-- 削除時に S3 のオブジェクトも消すため、キーを集める。
-- 外部キーの連鎖削除では S3 上の実体は消えない。
-- name: ListMediaKeysByPostID :many
SELECT m.s3_key AS s3_key
FROM media m
WHERE m.post_id = ?
UNION ALL
SELECT v.s3_key AS s3_key
FROM media_variants v
JOIN media m2 ON m2.id = v.media_id
WHERE m2.post_id = ?;

-- name: CreateMediaVariant :exec
INSERT INTO media_variants (media_id, kind, s3_key, width, height, bytes)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE s3_key = VALUES(s3_key), width = VALUES(width), height = VALUES(height), bytes = VALUES(bytes);

-- name: ListVariantsByPostID :many
SELECT v.media_id, v.kind, v.s3_key, v.width, v.height
FROM media_variants v
JOIN media m ON m.id = v.media_id
WHERE m.post_id = ?;

-- 複数の投稿の画像をまとめて取る。
--
-- **投稿ごとに問い合わせない（N+1 を作らない）。** フィードは1画面で
-- 20件返すため、投稿ごとに画像を引くと 20 回の往復になる。
-- name: ListMediaByPostIDs :many
SELECT id, post_id, width, height, s3_key
FROM media
WHERE post_id IN (sqlc.slice('post_ids'))
ORDER BY post_id, sort_order, id;

-- name: ListVariantsByPostIDs :many
SELECT v.media_id, m.post_id, v.kind, v.s3_key, v.width, v.height
FROM media_variants v
JOIN media m ON m.id = v.media_id
WHERE m.post_id IN (sqlc.slice('post_ids'));

-- どこからも参照されていない画像。
--
-- 投稿にも紐づかず、アバターにも使われていないものが対象。
-- **status では絞らない。** processed まで進んだが投稿されなかった
-- ものにも変換物があり、そちらが消えずに残るためである。
--
-- LIMIT で区切るのは、溜まっていた場合に1回の掃除が長時間の
-- トランザクションにならないようにするため。
-- name: ListOrphanMedia :many
SELECT m.id, m.s3_key
FROM media m
         LEFT JOIN users u ON u.avatar_media_id = m.id
WHERE m.post_id IS NULL
  AND u.id IS NULL
  AND m.created_at < ?
ORDER BY m.id
LIMIT ?;

-- name: ListVariantKeysByMediaID :many
SELECT s3_key
FROM media_variants
WHERE media_id = ?;

-- media_variants は外部キーの連鎖で消える。
-- name: DeleteMediaByID :exec
DELETE FROM media
WHERE id = ?;

-- 消すべき S3 のオブジェクトを控える。
--
-- **消す前に入れる。** 消してから入れるのでは、その間に落ちた場合に
-- 鍵を辿れなくなる（それが避けたい状態そのものである）。
-- name: EnqueueObjectDeletion :exec
INSERT INTO pending_object_deletions (s3_key)
VALUES (?)
ON DUPLICATE KEY UPDATE attempts = attempts + 1;

-- name: ListPendingObjectDeletions :many
SELECT id, s3_key
FROM pending_object_deletions
ORDER BY id
LIMIT ?;

-- name: DeletePendingObjectDeletion :exec
DELETE FROM pending_object_deletions
WHERE id = ?;

-- name: IncrementPendingObjectDeletionAttempts :exec
UPDATE pending_object_deletions
SET attempts = attempts + 1
WHERE id = ?;
