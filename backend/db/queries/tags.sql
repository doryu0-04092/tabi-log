-- タグ。名前は正規化（前後空白除去・NFKC・小文字化）してから渡すこと。

-- 既にあれば作らない。同時に同じタグが作られても一意制約で1つに収まる。
-- name: UpsertTag :execresult
INSERT INTO tags (name) VALUES (?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id);

-- name: AttachTagToPost :exec
INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)
ON DUPLICATE KEY UPDATE post_id = post_id;

-- name: DetachAllTagsFromPost :exec
DELETE FROM post_tags WHERE post_id = ?;

-- name: ListTagsByPostID :many
SELECT t.name
FROM tags t
JOIN post_tags pt ON pt.tag_id = t.id
WHERE pt.post_id = ?
ORDER BY t.name;

-- 複数の投稿のタグをまとめて取る（N+1 を避けるため）。
-- name: ListTagsByPostIDs :many
SELECT pt.post_id, t.name
FROM tags t
JOIN post_tags pt ON pt.tag_id = t.id
WHERE pt.post_id IN (sqlc.slice('post_ids'))
ORDER BY pt.post_id, t.name;
