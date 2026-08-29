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

-- ある利用者の投稿。新着フィードと同じくカーソルページネーション。
--
-- 索引は ix_posts_user_id (user_id, id DESC)。
-- **ix_posts_user_created (user_id, created_at DESC) では足りない。**
-- 絞り込みには使えるが ORDER BY id DESC を索引の並びで解決できず、
-- EXPLAIN に Using filesort が出る（2026-08-29 に実測して確認し、
-- 000003 で索引を追加した）。投稿数の多い利用者ほど並べ替える行が増える。
-- name: ListPostsByUserBefore :many
SELECT
    p.id, p.user_id, p.body, p.prefecture_code, p.spot_name, p.visited_on,
    p.like_count, p.comment_count, p.created_at, p.updated_at,
    u.handle, u.display_name, u.bio,
    pref.name AS prefecture_name, pref.name_kana AS prefecture_name_kana, pref.region
FROM posts p
JOIN users u ON u.id = p.user_id
JOIN prefectures pref ON pref.code = p.prefecture_code
WHERE p.user_id = ? AND p.id < ?
ORDER BY p.id DESC
LIMIT ?;

-- フォロー中フィード。
--
-- **IN (SELECT ...) ではなく follows との JOIN で書く。**
-- 意味は同じだが、MySQL は JOIN のほうが結合順を選べる。
--
-- **ここは負荷試験の対象である。** 想定は1人あたり平均200フォロー。
-- フォロー先は follows の主キー (follower_id, followee_id) で引けるが、
-- **全体を id 順に並べる部分は索引で解決できない。**
-- EXPLAIN に Using temporary; Using filesort が出る（2026-08-29 に実測）。
-- 複数の利用者の投稿を1つの順序に混ぜる以上、索引1本では避けられない。
-- 実測して厳しければ、フィードを事前に組み立てて持つ形（fan-out）へ
-- 作り方ごと変える判断になる。
-- name: ListFollowingFeedBefore :many
SELECT
    p.id, p.user_id, p.body, p.prefecture_code, p.spot_name, p.visited_on,
    p.like_count, p.comment_count, p.created_at, p.updated_at,
    u.handle, u.display_name, u.bio,
    pref.name AS prefecture_name, pref.name_kana AS prefecture_name_kana, pref.region
FROM posts p
JOIN follows f ON f.followee_id = p.user_id AND f.follower_id = ?
JOIN users u ON u.id = p.user_id
JOIN prefectures pref ON pref.code = p.prefecture_code
WHERE p.id < ?
ORDER BY p.id DESC
LIMIT ?;

-- ID を指定してまとめて取る。検索の結果を組み立てるために使う。
--
-- **並び順はここでは決めない。** 検索側が決めた順序を保つため、
-- 呼び出し元が ID の並びどおりに並べ直す。
-- name: ListPostsByIDs :many
SELECT
    p.id, p.user_id, p.body, p.prefecture_code, p.spot_name, p.visited_on,
    p.like_count, p.comment_count, p.created_at, p.updated_at,
    u.handle, u.display_name, u.bio,
    pref.name AS prefecture_name, pref.name_kana AS prefecture_name_kana, pref.region
FROM posts p
JOIN users u ON u.id = p.user_id
JOIN prefectures pref ON pref.code = p.prefecture_code
WHERE p.id IN (sqlc.slice('ids'));

-- 旅行履歴。**投稿日ではなく訪問日の新しい順。**
--
-- 訪問日は重複するため、カーソルは (訪問日, ID) の組になる。
--
-- **行値の比較 (a, b) < (?, ?) は使えない。** 意味としてはこちらが素直だが、
-- sqlc が解釈できずプレースホルダが引数に現れない（実測）。
-- 展開した形で書く。人気順のカーソルと同じ形である。
--
-- 索引 ix_posts_user_visited_id (user_id, visited_on DESC, id DESC)。
-- name: ListTravelsByUserBefore :many
SELECT
    p.id, p.user_id, p.body, p.prefecture_code, p.spot_name, p.visited_on,
    p.like_count, p.comment_count, p.created_at, p.updated_at,
    u.handle, u.display_name, u.bio,
    pref.name AS prefecture_name, pref.name_kana AS prefecture_name_kana, pref.region
FROM posts p
JOIN users u ON u.id = p.user_id
JOIN prefectures pref ON pref.code = p.prefecture_code
WHERE p.user_id = sqlc.arg('user_id')
  -- **訪問日の無い投稿は出さない。** 並べる軸そのものが無い。
  AND p.visited_on IS NOT NULL
  AND (p.visited_on < sqlc.arg('visited_on')
       OR (p.visited_on = sqlc.arg('visited_on') AND p.id < sqlc.arg('id')))
ORDER BY p.visited_on DESC, p.id DESC
LIMIT ?;
