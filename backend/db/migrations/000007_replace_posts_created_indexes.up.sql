-- 「絞り込みの列 + created_at」の索引を「絞り込みの列 + id」に置き換える。
--
-- **索引を足すだけでは足りず、古い方を落とす必要がある。**
-- 000003 で ix_posts_user_id (user_id, id DESC) を足したが、
-- ix_posts_user_created (user_id, created_at DESC) を残していた。
-- どちらも user_id の絞り込みには同じだけ効くため、
-- **オプティマイザは並べ替えを解決できない側を選ぶことがある。**
-- 実測（2026-08-29、投稿2万件・利用者40人）:
--
--   索引の指定なし   → key: ix_posts_user_created,  Extra: Using filesort
--   ix_posts_user_id を指定 → key: ix_posts_user_id, Extra: (filesort なし)
--
-- 000005 では ix_posts_user_visited を落として置き換えており、
-- 000003 だけ落とし忘れていた。ここで揃える。
--
-- 都道府県の絞り込みも同じ状態だったため、あわせて置き換える。
--
-- **代償を書いておく。** 人気順（`prefecture_code = ? AND created_at >= ?` を
-- like_count 順に並べる）は、期間の絞り込みを索引で行えなくなる。
-- ただし人気順は like_count による並べ替えが必ず入るため、
-- どちらの索引でも該当行を全部読む点は変わらない。
-- 一方、都道府県ごとの新着（id 順）は一覧の主要な導線であり、
-- そちらの並べ替えを消すほうを採る。

DROP INDEX ix_posts_user_created ON posts;

CREATE INDEX ix_posts_prefecture_id ON posts (prefecture_code, id DESC);
DROP INDEX ix_posts_prefecture_created ON posts;
