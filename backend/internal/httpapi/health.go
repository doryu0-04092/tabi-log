package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
)

// readyzTimeout は readyz が依存の確認に費やす上限時間である。
//
// ロードバランサのヘルスチェック間隔より十分に短くしておかないと、
// DB が応答しないときにチェックが積み上がって状況を悪化させる。
const readyzTimeout = 2 * time.Second

// Pinger は依存先への疎通確認を表す。
//
// *sql.DB を直接受け取らずインターフェースにしているのは、
// ヘルスチェックのテストに本物のデータベースを必要としないためである。
type Pinger interface {
	PingContext(ctx context.Context) error
}

// healthHandler は仕様（docs/openapi.yaml）の health タグのエンドポイントを実装する。
type healthHandler struct {
	db     Pinger
	logger *slog.Logger
}

// GetLivez はプロセスが生きていることだけを返す。**依存先を一切見ない。**
//
// ロードバランサのヘルスチェックはこちらに向ける。
//
// 単一のヘルスチェックで DB 疎通まで確認すると、DB が一時的に不調になったときに
// 全タスクが同時に unhealthy と判定され、一斉に置き換えが走る。
// 置き換えても DB は治らないため、状況は悪化するだけである。
// 「プロセスが生きているか」と「処理を受け付けられるか」は別の問いであり、
// 前者だけがタスクを入れ替えるべき理由になる。
func (h *healthHandler) GetLivez(w http.ResponseWriter, r *http.Request) {
	// 生成された型を使うことで、仕様のスキーマとレスポンスの形が食い違えば
	// コンパイルエラーになる。
	var body gen.LivezResponse
	body.Data.Status = gen.LivezResponseDataStatusOk
	writeJSON(w, r, http.StatusOK, body.Data)
}

// GetReadyz は依存先（現時点ではデータベース）への疎通を含めて確認する。
//
// デプロイ時の投入判定や、運用中の状況確認に使う。
// 疎通できない場合は 503 を返す。
func (h *healthHandler) GetReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		h.logger.WarnContext(r.Context(), "readyz: データベースへ疎通できない",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusServiceUnavailable, "dependency_unavailable", "データベースへ接続できません")
		return
	}

	var body gen.ReadyzResponse
	body.Data.Status = gen.ReadyzResponseDataStatusOk
	body.Data.Database = gen.ReadyzResponseDataDatabaseOk
	writeJSON(w, r, http.StatusOK, body.Data)
}
