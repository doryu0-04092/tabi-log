-- 都道府県マスタの参照。47件・不変のため参照系のみを置く。

-- name: ListPrefectures :many
SELECT code, name, name_kana, region, sort_order
FROM prefectures
ORDER BY sort_order;

-- name: GetPrefecture :one
SELECT code, name, name_kana, region, sort_order
FROM prefectures
WHERE code = ?;
