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
	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
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
	*searchHandler
	*notificationHandler
	*accountHandler
	*docsHandler
}

// Deps はルーターの構築に必要な依存をまとめる。
type Deps struct {
	// DB は疎通確認にのみ使う。データの読み書きは各 store が担う。
	DB            Pinger
	Prefectures   PrefectureLister
	Auth          AuthRepository
	Posts         PostRepository
	Reactions     ReactionRepository
	Follows       FollowRepository
	Search        SearchRepository
	Notifications NotificationRepository
	Account       AccountRepository
	Storage       ObjectStorage

	// CDNCookies は画像配信の署名付き Cookie を発行する係。
	// **nil でよい**（その場合は S3 の署名付き URL で配る）。
	CDNCookies *storage.CDNSigner
	// CDNCookieTTL は上の Cookie の有効期間。
	CDNCookieTTL time.Duration

	TokenIssuer   auth.TokenIssuer
	TokenVerifier auth.TokenVerifier
	AuthOptions   AuthOptions

	LoginAttemptLimit  int
	LoginAttemptWindow time.Duration

	// PostCreateLimit / CommentCreateLimit は認証済みの利用者が
	// WriteLimitWindow の間に作れる件数の上限。
	PostCreateLimit    int
	CommentCreateLimit int

	// UploadLimit は署名付き URL を発行できる回数の上限。
	//
	// **投稿の上限とは別に要る。** 発行1回ごとに S3 の PUT・Lambda の起動・
	// DB への行の追加が起きるため、投稿を作らなくても資源を消費できる。
	// 投稿1件につき最大4枚なので、投稿の上限より緩くする。
	UploadLimit int

	WriteLimitWindow time.Duration

	// Deletions は消し損ねた S3 のオブジェクトの控え先。
	// **nil でも動くが、削除に失敗した鍵は失われる。**
	Deletions DeletionQueue

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

	// アバターは投稿・コメント・通知・一覧のどこにも出てくる。
	// 引き方を1つに寄せ、各ハンドラは同じものを使う。
	avatars := &avatarResolver{
		repo:    deps.Account,
		follows: deps.Follows,
		storage: deps.Storage,
		logger:  deps.Logger,
	}

	srv := &server{
		healthHandler:     &healthHandler{db: deps.DB, logger: deps.Logger},
		prefectureHandler: &prefectureHandler{store: deps.Prefectures, logger: deps.Logger},
		authHandler: &authHandler{
			repo:    deps.Auth,
			issuer:  deps.TokenIssuer,
			opts:    deps.AuthOptions,
			logger:  deps.Logger,
			byIP:    NewRateLimiter(deps.LoginAttemptLimit, deps.LoginAttemptWindow),
			byEmail: NewRateLimiter(deps.LoginAttemptLimit, deps.LoginAttemptWindow),
			cdn: &cdnCookieIssuer{
				signer: deps.CDNCookies,
				ttl:    deps.CDNCookieTTL,
				secure: deps.AuthOptions.CookieSecure,
				logger: deps.Logger,
			},
			now:      time.Now,
			newToken: auth.NewRefreshToken,
		},
		postHandler: &postHandler{
			repo:    deps.Posts,
			storage: deps.Storage,
			likes:   deps.Reactions,
			avatars: avatars,
			logger:  deps.Logger,
			now:     time.Now,
			createLimit: &writeLimiter{
				limiter: NewRateLimiter(deps.PostCreateLimit, deps.WriteLimitWindow),
				message: "投稿の数が多すぎます。しばらく待ってからお試しください",
				logger:  deps.Logger,
				kind:    "post",
			},
			deletions: deps.Deletions,
			uploadLimit: &writeLimiter{
				limiter: NewRateLimiter(deps.UploadLimit, deps.WriteLimitWindow),
				message: "画像の送信が多すぎます。しばらく待ってからお試しください",
				logger:  deps.Logger,
				kind:    "upload",
			},
		},
		reactionHandler: &reactionHandler{
			repo:    deps.Reactions,
			posts:   deps.Posts,
			avatars: avatars,
			logger:  deps.Logger,
			createLimit: &writeLimiter{
				limiter: NewRateLimiter(deps.CommentCreateLimit, deps.WriteLimitWindow),
				message: "コメントの数が多すぎます。しばらく待ってからお試しください",
				logger:  deps.Logger,
				kind:    "comment",
			},
		},
		userHandler: &userHandler{
			repo:    deps.Follows,
			posts:   deps.Posts,
			likes:   deps.Reactions,
			storage: deps.Storage,
			avatars: avatars,
			logger:  deps.Logger,
		},
		accountHandler: &accountHandler{
			repo:      deps.Account,
			posts:     deps.Posts,
			likes:     deps.Reactions,
			follows:   deps.Follows,
			storage:   deps.Storage,
			deletions: deps.Deletions,
			avatars:   avatars,
			opts:      deps.AuthOptions,
			logger:    deps.Logger,
			now:       time.Now,
		},
		notificationHandler: &notificationHandler{
			repo:    deps.Notifications,
			avatars: avatars,
			logger:  deps.Logger,
			now:     time.Now,
		},
		docsHandler: &docsHandler{},
		searchHandler: &searchHandler{
			repo:    deps.Search,
			posts:   deps.Posts,
			likes:   deps.Reactions,
			follows: deps.Follows,
			storage: deps.Storage,
			avatars: avatars,
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

	// 並びは外側から内側。**最後のものが mux に最も近い。**
	// 404 と 405 の差し替えは ServeMux の書き出しを横取りするため、
	// mux のすぐ外側でなければ効かない。
	return chain(mux,
		WithRequestID,
		WithRecovery(deps.Logger),
		WithAccessLog(deps.Logger),
		WithAuthentication(deps.TokenVerifier, deps.Logger),
		WithJSONMuxErrors,
	)
}
