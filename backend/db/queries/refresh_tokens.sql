-- リフレッシュトークン。保存しているのは SHA-256 ハッシュのみで、平文は持たない。

-- name: CreateRefreshToken :execresult
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES (?, ?, ?);

-- ローテーションのために行を固定する。
--
-- FOR UPDATE が要点である。これが無いと、同時に届いた2つのリフレッシュが
-- 両方とも「まだ失効していない」と読み、どちらも正規のローテーションとして
-- 通ってしまう。行ロックで直列化し、片方を必ず「失効済みの提示」側へ回す。
-- name: GetRefreshTokenByHashForUpdate :one
SELECT id, user_id, token_hash, expires_at, revoked_at, replaced_by, created_at
FROM refresh_tokens
WHERE token_hash = ?
FOR UPDATE;

-- 失効させ、後継を記録する。
--
-- replaced_by は「正規のローテーションで置き換えられた」ことの根拠になる。
-- これが無いと、タブを複数開いた利用者の同時リフレッシュを
-- 盗用と誤判定して全ログアウトさせてしまう。
-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = ?, replaced_by = ?
WHERE id = ? AND revoked_at IS NULL;

-- 盗用を検知したときと、パスワード変更時に使う。
-- name: RevokeAllRefreshTokensForUser :exec
UPDATE refresh_tokens
SET revoked_at = ?
WHERE user_id = ? AND revoked_at IS NULL;

-- ログアウト。冪等にするため、既に失効していてもエラーにしない。
-- name: RevokeRefreshTokenByHash :exec
UPDATE refresh_tokens
SET revoked_at = ?
WHERE token_hash = ? AND revoked_at IS NULL;

-- 期限が切れたトークンの行を消す。
--
-- **失効済みでも、期限内のものは消さない。** 盗用の検知は
-- 「失効済みトークンの再提示」で判定しており、行を消すと
-- 再提示が「知らないトークン」に見えて検知できなくなる。
-- 期限が過ぎたトークンは提示されても通らないため、消してよい。
-- name: DeleteExpiredRefreshTokens :execresult
DELETE FROM refresh_tokens
WHERE expires_at < ?
LIMIT ?;
