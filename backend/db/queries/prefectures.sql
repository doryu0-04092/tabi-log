-- 都道府県マスタの参照。47件・不変のため参照系のみを置く。

-- name: ListPrefectures :many
SELECT code, name, name_kana, region, sort_order
FROM prefectures
ORDER BY sort_order;

-- name: GetPrefecture :one
SELECT code, name, name_kana, region, sort_order
FROM prefectures
WHERE code = ?;

-- ある利用者の、都道府県ごとの投稿数。
--
-- **投稿が無い県も 0 件として返す。** 制覇マップは47件すべてのマスを描くため、
-- LEFT JOIN で全県を残す。INNER JOIN にすると訪問済みの県しか返らず、
-- 画面側で都道府県マスタと突き合わせる処理が要る。
--
-- 索引 ix_posts_user_prefecture (user_id, prefecture_code) が効く。
-- name: ListPrefectureCountsByUser :many
SELECT
    p.code, p.name, p.region,
    COUNT(po.id) AS post_count
FROM prefectures p
LEFT JOIN posts po ON po.prefecture_code = p.code AND po.user_id = ?
GROUP BY p.code, p.name, p.region, p.sort_order
ORDER BY p.sort_order;
