-- tabi-log 初期スキーマ
--
-- 設計の根拠は docs/er-diagram.md に記す。ここには DDL のみを置く。
--
-- 日時列は DATETIME(6) とし、既定値の CURRENT_TIMESTAMP(6) はセッションの
-- タイムゾーンに従う。アプリケーションは接続文字列で time_zone を '+00:00' に
-- 固定しているため、これらは常に UTC で記録される（internal/config/config.go 参照）。

-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    handle          VARCHAR(30)     NOT NULL,
    email           VARCHAR(255)    NOT NULL,
    password_hash   VARCHAR(255)    NOT NULL,
    display_name    VARCHAR(50)     NOT NULL,
    bio             VARCHAR(300)        NULL,
    -- media への外部キーは media テーブル作成後に ALTER で追加する
    -- （users と media が相互に参照するため）
    avatar_media_id BIGINT UNSIGNED     NULL,
    created_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at      DATETIME(6)         NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_handle (handle),
    UNIQUE KEY uq_users_email (email),
    KEY ix_users_deleted_at (deleted_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- refresh_tokens
--
-- token_hash はトークン本体の SHA-256（16進64文字）である。
-- 本体を保存しないため、この表が漏洩してもトークンとして再利用できない。
--
-- replaced_by はローテーション後の後継を指す。これが無いと、タブを複数開いた
-- 利用者の同時リフレッシュを「盗用」と誤判定して全ログアウトさせてしまう。
-- ---------------------------------------------------------------------------
CREATE TABLE refresh_tokens (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    token_hash  CHAR(64)        NOT NULL,
    expires_at  DATETIME(6)     NOT NULL,
    revoked_at  DATETIME(6)         NULL,
    replaced_by BIGINT UNSIGNED     NULL,
    created_at  DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_refresh_tokens_hash (token_hash),
    KEY ix_refresh_tokens_user (user_id, revoked_at),
    KEY ix_refresh_tokens_expires (expires_at),
    CONSTRAINT fk_refresh_tokens_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_refresh_tokens_replaced_by
        FOREIGN KEY (replaced_by) REFERENCES refresh_tokens (id) ON DELETE SET NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- prefectures — 47件の不変マスタ。アプリケーションからは追加・更新・削除しない。
-- code は JIS X 0401 の都道府県コード。独自採番はしない。
-- ---------------------------------------------------------------------------
CREATE TABLE prefectures (
    code       CHAR(2)     NOT NULL,
    name       VARCHAR(10) NOT NULL,
    name_kana  VARCHAR(20) NOT NULL,
    region     VARCHAR(10) NOT NULL,
    sort_order SMALLINT    NOT NULL,
    PRIMARY KEY (code),
    KEY ix_prefectures_region (region, sort_order)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- posts
--
-- like_count / comment_count はカウンタ列である。フィードは1画面で20件返すため、
-- 都度 COUNT すると20件それぞれに集計が走る。いいね・コメントの登録および削除と
-- 同一トランザクションで増減させる。
-- ---------------------------------------------------------------------------
CREATE TABLE posts (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id         BIGINT UNSIGNED NOT NULL,
    body            VARCHAR(1000)   NOT NULL,
    prefecture_code CHAR(2)         NOT NULL,
    spot_name       VARCHAR(100)        NULL,
    visited_on      DATE            NOT NULL,
    like_count      INT UNSIGNED    NOT NULL DEFAULT 0,
    comment_count   INT UNSIGNED    NOT NULL DEFAULT 0,
    created_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    -- 新着フィードのカーソルページネーション
    KEY ix_posts_created (created_at DESC, id DESC),
    -- プロフィールの投稿一覧
    KEY ix_posts_user_created (user_id, created_at DESC),
    -- プロフィールの旅行履歴（訪問日順）
    KEY ix_posts_user_visited (user_id, visited_on DESC),
    -- 都道府県での絞り込み
    KEY ix_posts_prefecture_created (prefecture_code, created_at DESC),
    -- 都道府県制覇マップ（訪問済みの県を数える）
    KEY ix_posts_user_prefecture (user_id, prefecture_code),
    -- 人気順
    KEY ix_posts_like_count (like_count DESC, id DESC),
    -- 日本語は空白で区切られないため、既定のパーサでは全文検索が機能しない。
    -- ngram パーサは既定でトークン長2で全文を刻む。
    FULLTEXT KEY ft_posts_text (body, spot_name) WITH PARSER ngram,
    CONSTRAINT fk_posts_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_posts_prefecture
        FOREIGN KEY (prefecture_code) REFERENCES prefectures (code)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- media — 投稿画像とアバターの両方を扱う
--
-- status は「実行される予定」を表す状態である。署名付き URL を発行した時点で
-- pending として先に記録し（write-ahead）、確定しなかったものを後から特定できる
-- ようにする。これが無いと、送信後に投稿が確定されなかった分だけ S3 に
-- 誰も参照しないオブジェクトが蓄積する。
--
-- processed になったメディアだけが投稿として公開できる。EXIF の GPS 座標を
-- 除去する処理が終わっていない画像が配信される経路を作らないためである。
-- ---------------------------------------------------------------------------
CREATE TABLE media (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id    BIGINT UNSIGNED NOT NULL,
    post_id    BIGINT UNSIGNED     NULL,
    s3_key     VARCHAR(255)    NOT NULL,
    -- mime はサーバー側の検証後に確定する。クライアントの申告値は保存しない。
    mime       VARCHAR(50)         NULL,
    width      INT UNSIGNED        NULL,
    height     INT UNSIGNED        NULL,
    bytes      INT UNSIGNED        NULL,
    -- 署名付き URL の発行時点では未入力のため NULL を許す。
    -- 投稿に紐づくメディアではアプリケーション層で必須として検証する。
    alt_text   VARCHAR(200)        NULL,
    sort_order TINYINT UNSIGNED NOT NULL DEFAULT 0,
    status     ENUM ('pending', 'uploaded', 'processed', 'failed') NOT NULL DEFAULT 'pending',
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_media_s3_key (s3_key),
    KEY ix_media_post (post_id, sort_order),
    -- 確定しなかった pending を掃除するための索引
    KEY ix_media_status_created (status, created_at),
    KEY ix_media_user (user_id),
    CONSTRAINT fk_media_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_media_post
        FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- users から media への外部キー。media 作成後に追加する。
-- アバターを削除しても利用者は残るため ON DELETE SET NULL とする。
ALTER TABLE users
    ADD CONSTRAINT fk_users_avatar_media
        FOREIGN KEY (avatar_media_id) REFERENCES media (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- media_variants — 変換後の画像。kind を増やせば別形式・別解像度を追加できる。
-- ---------------------------------------------------------------------------
CREATE TABLE media_variants (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    media_id   BIGINT UNSIGNED NOT NULL,
    kind       ENUM ('thumb', 'medium', 'original') NOT NULL,
    s3_key     VARCHAR(255)    NOT NULL,
    width      INT UNSIGNED    NOT NULL,
    height     INT UNSIGNED    NOT NULL,
    bytes      INT UNSIGNED    NOT NULL,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_media_variants_media_kind (media_id, kind),
    CONSTRAINT fk_media_variants_media
        FOREIGN KEY (media_id) REFERENCES media (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- tags / post_tags
--
-- MySQL には配列型が無い。posts にカンマ区切りで持つとタグでの絞り込みが
-- 全走査になるため、最初から中間テーブルにする。
-- ---------------------------------------------------------------------------
CREATE TABLE tags (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name       VARCHAR(50)     NOT NULL,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_tags_name (name)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE post_tags (
    post_id BIGINT UNSIGNED NOT NULL,
    tag_id  BIGINT UNSIGNED NOT NULL,
    -- 主キーは「投稿からタグを引く」向き、追加の索引は「タグから投稿を引く」向き。
    -- タグでの絞り込みには後者が効く。
    PRIMARY KEY (post_id, tag_id),
    KEY ix_post_tags_tag (tag_id, post_id),
    CONSTRAINT fk_post_tags_post
        FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT fk_post_tags_tag
        FOREIGN KEY (tag_id) REFERENCES tags (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- likes
--
-- 主キーの列順が (user_id, post_id) であることに意味がある。フィードでは
-- 「表示する20件のうち自分がいいね済みなのはどれか」を
-- WHERE user_id = ? AND post_id IN (...) で求めるため、この順でないと効かない。
-- 複合主キーにより二重いいねも同時に防いでいる。
-- ---------------------------------------------------------------------------
CREATE TABLE likes (
    user_id    BIGINT UNSIGNED NOT NULL,
    post_id    BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_id, post_id),
    KEY ix_likes_post (post_id, created_at),
    CONSTRAINT fk_likes_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_likes_post
        FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- comments — 返信ツリーは対象外のため親コメントへの参照列は持たない
-- ---------------------------------------------------------------------------
CREATE TABLE comments (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    post_id    BIGINT UNSIGNED NOT NULL,
    user_id    BIGINT UNSIGNED NOT NULL,
    body       VARCHAR(500)    NOT NULL,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY ix_comments_post (post_id, created_at, id),
    KEY ix_comments_user (user_id),
    CONSTRAINT fk_comments_post
        FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT fk_comments_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- follows
--
-- 主キーが「自分がフォローしている人」（フォロー中フィードの起点）、
-- 追加の索引が「自分をフォローしている人」（フォロワー一覧）に対応する。
-- ---------------------------------------------------------------------------
CREATE TABLE follows (
    follower_id BIGINT UNSIGNED NOT NULL,
    followee_id BIGINT UNSIGNED NOT NULL,
    created_at  DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (follower_id, followee_id),
    KEY ix_follows_followee (followee_id, follower_id),
    CONSTRAINT fk_follows_follower
        FOREIGN KEY (follower_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_follows_followee
        FOREIGN KEY (followee_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT ck_follows_not_self CHECK (follower_id <> followee_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- notifications
--
-- 通知は本体の変更（いいね・コメント・フォロー）と同一トランザクションで
-- INSERT する。キューや Outbox は使わない。「いいねは記録されたが通知が消えた」
-- 状態を作らないための最も単純な方法である。
--
-- delivered_at は現時点では常に NULL である。将来プッシュ通知を足すときに
-- 「作られたが未送信」を表す列が必要になるが、その時点で20万行規模のテーブルへ
-- ALTER するのを避けるため、未使用のまま最初から置いておく。
-- ---------------------------------------------------------------------------
CREATE TABLE notifications (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id      BIGINT UNSIGNED NOT NULL,
    actor_id     BIGINT UNSIGNED NOT NULL,
    type         ENUM ('like', 'comment', 'follow') NOT NULL,
    post_id      BIGINT UNSIGNED     NULL,
    comment_id   BIGINT UNSIGNED     NULL,
    read_at      DATETIME(6)         NULL,
    delivered_at DATETIME(6)         NULL,
    created_at   DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY ix_notifications_user_created (user_id, created_at DESC, id DESC),
    KEY ix_notifications_user_read (user_id, read_at),
    CONSTRAINT fk_notifications_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_actor
        FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_post
        FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_comment
        FOREIGN KEY (comment_id) REFERENCES comments (id) ON DELETE CASCADE,
    CONSTRAINT ck_notifications_not_self CHECK (user_id <> actor_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
