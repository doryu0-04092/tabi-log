-- 000001_init の巻き戻し。
-- 外部キーの依存があるため、作成と逆順に落とす。
-- users は media を参照しているため、先に制約だけ外す。

ALTER TABLE users DROP FOREIGN KEY fk_users_avatar_media;

DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS follows;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS likes;
DROP TABLE IF EXISTS post_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS media_variants;
DROP TABLE IF EXISTS media;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS prefectures;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
