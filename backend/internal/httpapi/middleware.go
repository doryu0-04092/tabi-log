package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// requestIDHeader はリクエスト追跡 ID を返すレスポンスヘッダー名である。
const requestIDHeader = "X-Request-Id"

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFrom は context からリクエスト ID を取り出す。無い場合は空文字を返す。
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// newRequestID は 128 ビットのランダムな 16 進文字列を返す。
//
// UUID ライブラリを入れていないのは、必要なのは「衝突しない識別子」だけであり
// crypto/rand で十分なためである。依存を1つ減らしている。
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand が失敗する状況では追跡 ID どころではないが、
		// ここで落とすとリクエスト全体が失われるため時刻由来の値で継続する。
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

// WithRequestID はリクエストごとに追跡 ID を発行し、context とレスポンスヘッダーに載せる。
//
// 複数の利用者のリクエストが同時に処理されるため、追跡 ID が無いと
// 「どのログがどのリクエストのものか」を後から追えない。
// 全ミドルウェアの最も外側に置く。
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder は書き込まれたステータスコードを記録する ResponseWriter である。
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(b)
}

// WithAccessLog はアクセスログを1行出力する。
//
// 出力するのはメソッド・パス・ステータス・所要時間・追跡 ID のみである。
// ヘッダー・Cookie・ボディ・クエリ文字列は**構造的に渡さない**。
// マスキング方式は書き漏らしで秘密が漏れるため、
// 認証トークンやパスワードがログに入る経路自体を作らない設計にしている。
func WithAccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path), // クエリ文字列は意図的に含めない
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// WithRecovery はハンドラの panic を捕捉し、500 を返してプロセスの停止を防ぐ。
//
// panic をそのまま伝播させるとサーバー全体が落ち、
// 1件のバグで全利用者のリクエストが失われる。
func WithRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.LogAttrs(r.Context(), slog.LevelError, "panic_recovered",
						slog.String("request_id", RequestIDFrom(r.Context())),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())),
					)
					// 内部の詳細は利用者に返さない。
					writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// chain はミドルウェアを外側から順に適用する。
//
// chain(h, a, b) は a(b(h)) と等しく、a が最も外側になる。
func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
