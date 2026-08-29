package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
)

const userIDKey contextKey = "user_id"

// UserIDFrom は context からログイン中の利用者IDを取り出す。
// 認証を要するエンドポイントでは必ず存在する。
func UserIDFrom(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(userIDKey).(uint64)
	return id, ok
}

// publicPaths は認証を要さないパスの一覧である。
//
// **既定は「認証が要る」であり、ここに書いたものだけが例外になる。**
// 逆にすると、新しいエンドポイントを足したときに書き忘れが
// 「認証なしで公開」になる。書き忘れの結果が安全側へ倒れる向きにしている。
//
// docs/openapi.yaml の `security: []` を付けた操作と一致していること。
// 一致は authmiddleware_test.go が検証する。
var publicPaths = map[string]struct{}{
	"/api/livez":        {},
	"/api/readyz":       {},
	"/api/prefectures":  {},
	"/api/auth/signup":  {},
	"/api/auth/login":   {},
	"/api/auth/refresh": {},
	"/api/auth/logout":  {},
	// 仕様書そのもの。**認可を仕様の秘匿に依存させていない**ため公開する
	// （docs/openapi.yaml の /docs の説明を参照）。
	"/api/docs":         {},
	"/api/openapi.yaml": {},
}

// WithAuthentication はアクセストークンを検証し、利用者IDを context に載せる。
//
// publicPaths に無いパスは、有効なトークンが無ければ 401 を返す。
//
// **断った理由は debug で残す。** 利用者へ返す文言は
// 「ログインが必要です」に丸めており、期限切れなのか署名が違うのかが
// 応答からは分からない。丸めているのは総当たりの手がかりを与えないためで、
// **こちら側でも分からないままにする理由は無い。**
// 既定（info）では出ないので、通常の運用でログが埋まることもない。
func WithAuthentication(verifier auth.TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, public := publicPaths[r.URL.Path]; public {
				next.ServeHTTP(w, r)
				return
			}

			token, ok := bearerToken(r)
			if !ok {
				debugAuth(r, logger, "Authorization ヘッダーが無いか形式が違う")
				writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
				return
			}

			claims, err := verifier.Verify(token)
			if err != nil {
				// 期限切れだけは区別して返す。クライアントが
				// 「リフレッシュすべき」か「再ログインが要る」かを判断できないと、
				// 期限が切れるたびに利用者を再ログインさせることになる。
				if errors.Is(err, auth.ErrTokenExpired) {
					debugAuth(r, logger, "アクセストークンの期限が切れている")
					writeError(w, r, http.StatusUnauthorized, "token_expired", "アクセストークンの期限が切れています")
					return
				}
				// **トークンそのものは絶対に載せない。** 有効なものが
				// 混ざっていれば、ログを読める人がなりすませる。
				debugAuth(r, logger, "アクセストークンを検証できない: "+err.Error())
				writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// debugAuth は認証を断った理由を残す。
//
// 載せるのはメソッドとパスと理由だけにする。**ヘッダーもトークンも渡さない。**
func debugAuth(r *http.Request, logger *slog.Logger, reason string) {
	if logger == nil {
		return
	}
	logger.DebugContext(r.Context(), "認証を断った",
		slog.String("reason", reason),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// bearerToken は Authorization ヘッダーからトークンを取り出す。
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	v := r.Header.Get("Authorization")
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(v[len(prefix):])
	return token, token != ""
}

// csrfHeaderName / csrfHeaderValue は Cookie を使うエンドポイントの CSRF 対策。
const (
	csrfHeaderName  = "X-Requested-With"
	csrfHeaderValue = "tabi-log"
)

// requireCSRFHeader は Cookie で認証するエンドポイントを守る。
//
// カスタムヘッダーは単純リクエストの条件を外れるため、クロスオリジンから
// 送るには CORS のプリフライトが通る必要がある。フォーム送信や <img> のような
// **プリフライトを伴わない経路では付けられない**。
//
// SameSite=Strict と合わせて二重に守っている。片方だけに頼らないのは、
// SameSite の解釈がブラウザによって差があるためである。
func requireCSRFHeader(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(csrfHeaderName) != csrfHeaderValue {
		writeError(w, r, http.StatusForbidden, "csrf_rejected",
			"リクエストが拒否されました")
		return false
	}
	return true
}
