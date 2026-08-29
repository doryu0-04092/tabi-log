package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
)

/*
投稿とコメントの作成にかける上限。

**ログイン試行の上限とは目的が違う。** ログイン側は「他人になりすます
試行」を止めるためのもので、鍵は IP とアカウントの両方に置く。
こちらは**認証済みの利用者が大量に書き込むこと**を止めるためのもので、
鍵は利用者 ID だけでよい。IP で数えると、同じ回線の別の利用者
（学校・職場・携帯網）が巻き添えになる。

上限の値は「人が手で操作する速さの数倍」を目安に置いている。
実測に基づく数字ではないため、**運用が始まったら実際の分布を見て
決め直す**（docs/operations.md）。厳しすぎる上限は、
攻撃者ではなく普通の利用者を止めることになる。

**プロセスのメモリで数える制約は RateLimiter と同じである。**
タスクが複数あると各タスクが独立に数えるため、実効的な上限は
タスク数の倍になる。
*/

// 既定値は internal/config が持つ（POST_CREATE_LIMIT / COMMENT_CREATE_LIMIT /
// WRITE_LIMIT_WINDOW）。**ここには置かない。** 2か所に書くと、
// 片方だけ変えたときに「設定したはずの値と違う」ことになる。

// writeLimiter は書き込みの上限を1種類ぶん持つ。
type writeLimiter struct {
	limiter *RateLimiter
	// logger は弾いたことを記録する先。
	//
	// **弾いた事実が残らないと、429 が増えたときに
	// 「攻撃されている」のか「上限が厳しすぎる」のかを判断できない。**
	// どちらも利用者から見れば同じ画面になる。
	logger *slog.Logger
	// kind はどの上限かを記録に残すための名前。
	kind string
	// message は上限に達したときに利用者へ返す文言。
	// **何をどうすればよいかまで書く。** 「制限されました」だけでは
	// 待てばよいのか、やり方が悪いのかが分からない。
	message string
}

// allow は1回ぶんを記録し、超えていれば 429 を返して false を返す。
//
// **鍵に利用者 ID を使う。** ここに到達する時点で認証は済んでいる。
func (l *writeLimiter) allow(w http.ResponseWriter, r *http.Request, userID uint64) bool {
	if l == nil || l.limiter == nil {
		return true
	}
	if l.limiter.Allow(writeKey(userID)) {
		return true
	}

	// **警告として記録する。** エラーではない（想定どおりに働いている）が、
	// 正常でもない。利用者 ID だけを載せる。本文は載せない。
	if l.logger != nil {
		l.logger.WarnContext(r.Context(), "書き込みの上限で拒否した",
			slog.String("kind", l.kind),
			slog.Uint64("user_id", userID),
		)
	}

	writeError(w, r, http.StatusTooManyRequests, "rate_limited", l.message)
	return false
}

// writeKey は数える鍵を組み立てる。
//
// 種類ごとに別の RateLimiter を持つため、鍵に種類を混ぜる必要は無い。
func writeKey(userID uint64) string {
	return strconv.FormatUint(userID, 10)
}
