-- 都道府県マスタの投入を巻き戻す。
-- posts が参照している場合は外部キー制約により削除できない（意図した動作）。
DELETE FROM prefectures;
