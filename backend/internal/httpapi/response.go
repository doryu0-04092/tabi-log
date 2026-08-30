package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

// ---------------------------------------------------------------------------
// 相手が待つのをやめたとき
// ---------------------------------------------------------------------------

/*
利用者が読み込みの途中で画面を移ると、リクエストは打ち切られる。
そのとき `context.Canceled` がデータベースの呼び出しから返ってくる。

**これはサーバーの失敗ではない。** それでも 500 を返して ERROR で記録すると、
2つの実害が出る。

  1. **アラートが誤報になる。** 運用設計書では 5xx 率が 5% を超えたら
     即時通知（P1）にしている。画面を移るだけで積み上がるのでは、
     鳴っても見なくなる
  2. **本物の ERROR が埋もれる。** 調べるときに最初に見るのが ERROR なので、
     そこに「誰も待っていない失敗」が混ざると絞り込みが効かない

実際、E2E を重い処理と同時に走らせたときにこの形で 500 が並んだ
（2026-08-29）。**サーバーは正常なのに壊れて見えた。**
*/

// StatusClientClosedRequest は「応答を待つ相手がもういない」ことを表す。
//
// **標準の HTTP には無い。** nginx が使い始めた 499 を借りている。
// 借りているのは、**5xx にも 2xx にも入れたくない**ためである。
// 何も書かずに返すと、記録上は 200（成功）になってしまう。
const StatusClientClosedRequest = 499

// clientGone は「相手が待つのをやめた」ことによる失敗かどうかを返す。
//
// **エラーの型だけでは判断しない。** context.Canceled は
// こちらが自分で打ち切った場合にも返る。リクエストの context が
// 実際に終了していることまで見て、初めて「相手が切った」と言える。
func clientGone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && ctx.Err() != nil
}

// failureLevel は失敗を記録する重さを返す。
//
// 応答を返さない場所（一覧の一部が取れなかった等）で使う。
func failureLevel(ctx context.Context, err error) slog.Level {
	if clientGone(ctx, err) {
		return slog.LevelDebug
	}
	return slog.LevelError
}

// respondInternalError は失敗を記録し、利用者には内容を明かさずに返す。
//
// **6つのハンドラが同じものを持っていたので1つに寄せた。**
// 相手が切った場合の扱いを変えるにあたり、6か所を同じように直す必要があり、
// 揃っていない状態のまま置いておく理由が無くなったためである。
func respondInternalError(
	w http.ResponseWriter, r *http.Request, logger *slog.Logger, msg string, err error,
) {
	attrs := []any{slog.String("request_id", RequestIDFrom(r.Context()))}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}

	if clientGone(r.Context(), err) {
		// **本文を書かない。** 読む相手がいない。
		logger.DebugContext(r.Context(), msg+"（相手が待つのをやめた）", attrs...)
		w.WriteHeader(StatusClientClosedRequest)
		return
	}

	logger.ErrorContext(r.Context(), msg, attrs...)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}
