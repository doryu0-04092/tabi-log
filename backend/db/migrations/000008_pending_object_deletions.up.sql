-- 消し損ねた S3 のオブジェクトを覚えておく。
--
-- 投稿の削除・退会では、データベースを先に確定させ、そのあとで S3 を消す。
-- 「S3 は消えたがデータベースに残る」より「データベースは消えたが S3 に
-- 残る」ほうが害が小さいためである。
--
-- **だが、後者を拾う仕組みが無かった。** 行が消えた時点で鍵を辿れなくなり、
-- S3 の削除が失敗するとオブジェクトが永久に残る。原本には
-- state=kept が付いておりライフサイクルの対象外で、変換物には
-- ライフサイクル自体が無い（消すと表示中の投稿が壊れるため置けない）。
--
-- 消す前に鍵をここへ入れ、消せたら消す。掃除が残りを拾う。
CREATE TABLE pending_object_deletions (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    s3_key     VARCHAR(255)    NOT NULL,
    -- 何度も失敗するものを見分けるために数える。
    attempts   INT UNSIGNED    NOT NULL DEFAULT 0,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    -- 同じ鍵を二重に入れない。**削除は冪等だが、行が増えるのは無駄。**
    UNIQUE KEY uq_pending_object_deletions_key (s3_key),
    KEY ix_pending_object_deletions_created (created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
