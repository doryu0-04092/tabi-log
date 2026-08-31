-- コメントの索引を (post_id, created_at, id) から (post_id, id) に置き換える。
--
-- 一覧の問い合わせは次の形である（db/queries/comments.sql）。
--
--     WHERE c.post_id = ? AND c.id > ? ORDER BY c.id LIMIT ?
--
-- 索引の2列目が created_at なので、post_id で絞ったあとの並びは
-- created_at → id になる。**id の順は索引から得られない。**
-- データベースは created_at の順と id の順が一致することを知らないためである。
--
-- **分布によって出方が変わる。** 実測（2026-09-01、MySQL 8.4）:
--
--   実態に近い分布（400 投稿に 25 件ずつ、計 1 万件）
--     → ix_comments_post を使うが Using filesort が出る
--
--   1 つの投稿に偏らせた分布（1 万件中 2000 件が対象の投稿）
--     → 並べ替えは出ないが key=PRIMARY / rows=4957 / filtered=20.17%。
--       索引を使わず id 順に走査し post_id を後から捨てている
--
-- **後者は Extra だけを見ていると気づけない。** 最初にこの分布で測り、
-- 「並べ替えが出ないので問題なし」と誤って判断した。
-- 検証は Extra だけでなく key も見るようにした（query_plan_test.go）。
--
-- created_at で並べる問い合わせは1つも無い。よって3列目ごと落とす。
-- 外部キー post_id に必要な「post_id で始まる索引」の条件も満たす。
ALTER TABLE comments
    DROP INDEX ix_comments_post,
    ADD INDEX ix_comments_post (post_id, id);
