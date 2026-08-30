package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
相手が待つのをやめたときの扱い。

**利用者が読み込みの途中で画面を移ると、リクエストは打ち切られる。**
これはサーバーの失敗ではない。それでも 500 を返して ERROR で記録すると、

  - 運用設計書のアラート（5xx 率 5% で即時通知）が誤報になる
  - 調べるときに最初に見る ERROR に、誰も待っていない失敗が混ざる

実際に E2E を重い処理と同時に走らせたとき、この形で 500 が並び、
**サーバーは正常なのに壊れて見えた**（2026-08-29）。
*/

// captureLogger は記録された内容を読めるようにする。
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, &buf
}

// canceledRequest は「相手が切った」状態のリクエストを作る。
func canceledRequest() *http.Request {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return httptest.NewRequest(http.MethodGet, "/api/posts", nil).WithContext(ctx)
}

func Test相手が切った失敗は5xxにしない(t *testing.T) {
	logger, logs := captureLogger()
	rec := httptest.NewRecorder()

	respondInternalError(rec, canceledRequest(), logger,
		"投稿の所有者を取得できない", fmt.Errorf("問い合わせに失敗した: %w", context.Canceled))

	// **5xx にしない。** アラートの根拠が誤報で埋まる。
	if rec.Code >= 500 {
		t.Errorf("状態が %d。5xx にしてはいけない", rec.Code)
	}
	// **200 にもしない。** 記録上は成功になってしまう。
	if rec.Code == http.StatusOK {
		t.Error("状態が 200。成功として記録されてしまう")
	}
	if rec.Code != StatusClientClosedRequest {
		t.Errorf("状態が %d。%d のはず", rec.Code, StatusClientClosedRequest)
	}

	// **本文を書かない。** 読む相手がいない。
	if rec.Body.Len() != 0 {
		t.Errorf("本文を書いている: %s", rec.Body.String())
	}

	// **ERROR で記録しない。** 本物の失敗が埋もれる。
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("ERROR で記録している: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"level":"DEBUG"`) {
		t.Errorf("DEBUG で記録していない: %s", logs.String())
	}
}

func Test本物の失敗はこれまでどおり500になる(t *testing.T) {
	logger, logs := captureLogger()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/posts", nil)

	respondInternalError(rec, r, logger, "投稿の取得に失敗した", errors.New("接続が切れている"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("状態が %d。500 のはず", rec.Code)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("ERROR で記録していない: %s", logs.String())
	}

	// 利用者には中身を明かさない。
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("本文を読めない: %v", err)
	}
	if body.Error.Code != "internal_error" {
		t.Errorf("code が %q", body.Error.Code)
	}
	if strings.Contains(rec.Body.String(), "接続が切れている") {
		t.Error("内部のエラー文言が利用者に漏れている")
	}
}

/*
**エラーの型だけで判断しない。**

context.Canceled は、こちらが自分で打ち切った場合にも返る。
リクエストの context が生きているのにこれを「相手が切った」と扱うと、
**本物の失敗を握りつぶす**ことになる。
*/
func Test自分で打ち切った場合は相手のせいにしない(t *testing.T) {
	logger, logs := captureLogger()
	rec := httptest.NewRecorder()
	// context は生きている。エラーだけが context.Canceled。
	r := httptest.NewRequest(http.MethodGet, "/api/posts", nil)

	respondInternalError(rec, r, logger, "内部で打ち切った", context.Canceled)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("状態が %d。500 のはず（相手は待っている）", rec.Code)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("ERROR で記録していない: %s", logs.String())
	}
}

func Test記録する重さが状況で変わる(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want slog.Level
	}{
		{"相手が切った", canceled, context.Canceled, slog.LevelDebug},
		{"本物の失敗", canceled, errors.New("接続が切れている"), slog.LevelError},
		{"context は生きている", context.Background(), context.Canceled, slog.LevelError},
	}

	for _, tc := range cases {
		if got := failureLevel(tc.ctx, tc.err); got != tc.want {
			t.Errorf("%s: %v。%v のはず", tc.name, got, tc.want)
		}
	}
}

// **アクセスログに 499 が残ること。**
// 何も書かずに返すと 200 として記録され、失敗が成功に見える。
func Test相手が切った場合もアクセスログに残る(t *testing.T) {
	logger, logs := captureLogger()

	handler := WithAccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondInternalError(w, r, logger, "打ち切られた", context.Canceled)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, canceledRequest())

	if !strings.Contains(logs.String(), fmt.Sprintf(`"status":%d`, StatusClientClosedRequest)) {
		t.Errorf("アクセスログに %d が残っていない: %s", StatusClientClosedRequest, logs.String())
	}
}
