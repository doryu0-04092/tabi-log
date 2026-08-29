// Package httpapi は HTTP の受け口を組み立てる。
//
// ルーティングには標準ライブラリの http.ServeMux を使う。
// Go 1.22 以降の ServeMux は "GET /api/posts/{id}" のようなメソッド付きの
// パターンを扱えるため、外部のルーターを持ち込む理由がない。
//
// エンドポイントの登録そのものは OpenAPI 仕様から生成したコード
// （internal/api/gen）が行う。仕様にあるエンドポイントを実装し忘れると
// ServerInterface を満たせずコンパイルエラーになる。
// 仕様と実装のずれをレビューではなく型検査で捕まえるための構成である。
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
)

// apiBasePath は全エンドポイントの前置きである。
//
// CloudFront がこの接頭辞でバックエンドへ振り分けるため、
// 静的ファイルの配信経路と衝突しない。
const apiBasePath = "/api"

// server は仕様上の全エンドポイントを実装する。
//
// この宣言により、docs/openapi.yaml にエンドポイントを追加して再生成すると、
// 実装するまでビルドが通らなくなる。
var _ gen.ServerInterface = (*server)(nil)

// server は各機能のハンドラを埋め込み、gen.ServerInterface を満たす。
// 機能を追加するときはここに埋め込みを足す。
type server struct {
	*healthHandler
	*prefectureHandler
	*authHandler
	*postHandler
	*reactionHandler
	*userHandler
}

// Deps はルーターの構築に必要な依存をまとめる。
type Deps struct {
	// DB は疎通確認にのみ使う。データの読み書きは各 store が担う。
	DB          Pinger
	Prefectures PrefectureLister
	Auth        AuthRepository
	Posts       PostRepository
	Reactions   ReactionRepository
	Follows     FollowRepository
	Storage     ObjectStorage

	TokenIssuer   auth.TokenIssuer
	TokenVerifier auth.TokenVerifier
	AuthOptions   AuthOptions

	LoginAttemptLimit  int
	LoginAttemptWindow time.Duration

	Logger *slog.Logger
}

// NewRouter は全エンドポイントを登録した http.Handler を返す。
//
// ミドルウェアの適用順は外側から:
//  1. WithRequestID      — 追跡 ID を最初に発行する（以降のログすべてに載せるため）
//  2. WithRecovery       — panic を捕捉する（ログ出力より内側だと記録が残らない）
//  3. WithAccessLog      — 実際のステータスと所要時間を記録する
//  4. WithAuthentication — 認証。**publicPaths 以外は既定で拒否する**
func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()

	// 仕様に無いパスは JSON でエラーを返す。
	// 既定の http.NotFound は HTML を返すため、
	// JSON を期待しているクライアントが解釈に失敗する。
	//
	// 生成コードによる登録より先に置く。ServeMux はより具体的なパターンを
	// 優先するため、"/api/livez" 等は後から登録してもこちらより優先される。
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "エンドポイントが存在しません")
	})

	srv := &server{
		healthHandler:     &healthHandler{db: deps.DB, logger: deps.Logger},
		prefectureHandler: &prefectureHandler{store: deps.Prefectures, logger: deps.Logger},
		authHandler: &authHandler{
			repo:     deps.Auth,
			issuer:   deps.TokenIssuer,
			opts:     deps.AuthOptions,
			logger:   deps.Logger,
			byIP:     NewRateLimiter(deps.LoginAttemptLimit, deps.LoginAttemptWindow),
			byEmail:  NewRateLimiter(deps.LoginAttemptLimit, deps.LoginAttemptWindow),
			now:      time.Now,
			newToken: auth.NewRefreshToken,
		},
		postHandler: &postHandler{
			repo:    deps.Posts,
			storage: deps.Storage,
			likes:   deps.Reactions,
			logger:  deps.Logger,
			now:     time.Now,
		},
		reactionHandler: &reactionHandler{
			repo:   deps.Reactions,
			posts:  deps.Posts,
			logger: deps.Logger,
		},
		userHandler: &userHandler{
			repo:    deps.Follows,
			posts:   deps.Posts,
			likes:   deps.Reactions,
			storage: deps.Storage,
			logger:  deps.Logger,
		},
	}

	// ErrorHandlerFunc は、生成コードのパラメータ検証
	//（必須ヘッダーの欠落など）で失敗したときの応答を決める。
	//
	// **既定のハンドラは text/plain で 400 を返し、内部のエラー文言をそのまま含める。**
	// JSON を期待するクライアントが解釈に失敗するうえ、実装の詳細が漏れる。
	// 他のエラーと同じ形に揃えるため、必ず差し替える。
	gen.HandlerWithOptions(srv, gen.StdHTTPServerOptions{
		BaseURL:    apiBasePath,
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "リクエストの形式が正しくありません")
		},
	})

	return chain(mux,
		WithRequestID,
		WithRecovery(deps.Logger),
		WithAccessLog(deps.Logger),
		WithAuthentication(deps.TokenVerifier),
	)
}
