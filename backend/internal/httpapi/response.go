package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// successEnvelope は成功レスポンスの外枠である。
//
// 生の値ではなく常に `{"data": ...}` で包むのは、後からページネーション情報や
// メタデータを足すときにレスポンスの形を壊さずに済むためである。
type successEnvelope struct {
	Data any `json:"data"`
}

// errorEnvelope はエラーレスポンスの外枠である。
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	// Code はクライアントが分岐に使う機械可読な識別子である。
	// メッセージの文言変更で分岐が壊れないよう、必ずこちらで判定させる。
	Code string `json:"code"`
	// Message は利用者に表示する説明である。内部構造やスタックは含めない。
	Message string `json:"message"`
}

// writeJSON は任意の値を `{"data": ...}` で包んで返す。
func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(successEnvelope{Data: data}); err != nil {
		// ヘッダーは送出済みなのでステータスは変えられない。記録のみ行う。
		slog.ErrorContext(r.Context(), "レスポンスの書き込みに失敗した",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("error", err.Error()),
		)
	}
}

// writeError はエラーを `{"error": {...}}` で包んで返す。
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{Code: code, Message: message}}); err != nil {
		slog.ErrorContext(r.Context(), "エラーレスポンスの書き込みに失敗した",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("error", err.Error()),
		)
	}
}
