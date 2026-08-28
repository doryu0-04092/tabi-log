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

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
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
}

// Deps はルーターの構築に必要な依存をまとめる。
type Deps struct {
	// DB は疎通確認にのみ使う。データの読み書きは各 store が担う。
	DB          Pinger
	Prefectures PrefectureLister
	Logger      *slog.Logger
}

// NewRouter は全エンドポイントを登録した http.Handler を返す。
//
// ミドルウェアの適用順は外側から:
//  1. WithRequestID — 追跡 ID を最初に発行する（以降のログすべてに載せるため）
//  2. WithRecovery  — panic を捕捉する（ログ出力より内側だと記録が残らない）
//  3. WithAccessLog — 実際のステータスと所要時間を記録する
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
	}
	gen.HandlerFromMuxWithBaseURL(srv, mux, apiBasePath)

	return chain(mux,
		WithRequestID,
		WithRecovery(deps.Logger),
		WithAccessLog(deps.Logger),
	)
}
