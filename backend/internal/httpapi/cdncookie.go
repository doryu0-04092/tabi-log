package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
)

/*
CloudFront の署名付き Cookie を配る。

**画像の URL を固定するための仕掛けである。**
S3 の署名付き URL は呼ぶたびに変わるため、エッジにもブラウザにも
キャッシュが載らない。URL を `https://<配信ドメイン>/variants/<鍵>` に固定し、
読む権利を Cookie 側に移すことで、はじめてキャッシュが効くようになる。

**リフレッシュトークンの Cookie と同じ場所で置き直す。**
画面は15分ごとにアクセストークンを取り直すため、
使い続けている限りこの Cookie も切れない。
*/

// cdnCookieIssuer は署名付き Cookie を発行する。
//
// **nil を許す。** ローカルと LocalStack には CloudFront が無く、
// そちらは S3 の署名付き URL のまま動かす必要がある。
type cdnCookieIssuer struct {
	signer *storage.CDNSigner
	ttl    time.Duration
	secure bool
	logger *slog.Logger
}

// issue は Cookie を3つ置く。
//
// **失敗しても呼び出し側の処理は止めない。** ここが失敗して困るのは
// 画像が出ないことであり、ログインそのものを失敗させる理由にはならない。
// 記録は残す。
func (c *cdnCookieIssuer) issue(ctx context.Context, w http.ResponseWriter, now time.Time) {
	if c == nil || c.signer == nil {
		return
	}

	cookies, err := c.signer.SignedCookies(now.Add(c.ttl))
	if err != nil {
		c.logger.ErrorContext(ctx, "画像配信の Cookie を発行できない",
			slog.String("error", err.Error()))
		return
	}

	for _, ck := range cookies {
		http.SetCookie(w, &http.Cookie{
			Name:  ck.Name,
			Value: ck.Value,
			// **画像の取得にだけ送る。** API のリクエストに毎回3つ載せる
			// 必要はなく、載せれば載せただけ通信量になる。
			Path: "/" + storage.CDNAllowedPrefix,
			// 画面から読む必要は無い。
			HttpOnly: true,
			Secure:   c.secure,
			// **Strict にしない。** 外部リンクから開いたときの最初の
			// 描画で画像が出ないことになる。画像を取るだけの Cookie に
			// CSRF の危険は無い。
			SameSite: http.SameSiteLaxMode,
			// **Expires と Max-Age をあえて付けない。**
			//
			// AWS は「除外することを勧める。ブラウザを閉じたときに
			// Cookie が消え、第三者に使われる余地が減る」としている
			// （署名付き Cookie の設定手順）。
			//
			// **消えても困らない。** 次に開いたとき、画面は
			// セッションを復元するために /auth/refresh を呼び、
			// そこでこの Cookie も置き直される。
			//
			// なお有効期限そのものはポリシーの側（DateLessThan）が持つ。
			// 期限が切れた Cookie は、残っていても CloudFront が弾く。
		})
	}
}

// clear はログアウト時などに消す。
//
// **発行時と Path・属性を揃える。** 揃っていないとブラウザが
// 別の Cookie とみなし、元のものが残る。
func (c *cdnCookieIssuer) clear(w http.ResponseWriter) {
	if c == nil || c.signer == nil {
		return
	}

	for _, name := range storage.CDNCookieNames() {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/" + storage.CDNAllowedPrefix,
			HttpOnly: true,
			Secure:   c.secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
}
