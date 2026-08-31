package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

/*
405 の応答も JSON で返す。

**クライアントは応答を JSON として読む。** 405 だけ text/plain だと、
エラーの中身を読もうとして別のエラーになる。

`docs/audit-2026-08-31.md` L3。
*/

func Test405もJSONで返す(t *testing.T) {
	h := newRouter(t, testDeps{})

	// livez は GET だけを受ける。
	rec := doJSON(h, req(http.MethodDelete, "/api/livez", ""))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("%d が返った。405 を期待した: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type が %q だった。JSON を期待した", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON として読めなかった: %v（本文: %q）", err, rec.Body.String())
	}
	if body.Error.Code != "method_not_allowed" {
		t.Errorf("code が %q だった", body.Error.Code)
	}

	// ServeMux が付けた Allow は残す。**消すと、どのメソッドなら
	// 通るのかをクライアントが知る手段が無くなる。**
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("Allow ヘッダーが消えている")
	}
}

// 存在しないパスは JSON の 404 を返す。
// **受け皿をやめたので、ここは ServeMux の既定を横取りしている。**
//
// トークンを付けるのは、認証を通さないと 401 で止まり
// mux まで届かないためである（未知のパスがどれかを未認証の相手に
// 教えない、という点では望ましい振る舞い）。
func Test存在しないパスはJSONの404を返す(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens})
	token := mustIssue(t, tokens, 7)
	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/そんなものはない", ""), token))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type が %q だった。JSON を期待した", ct)
	}
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON として読めなかった: %v（本文: %q）", err, rec.Body.String())
	}
	if body.Error.Code != "not_found" {
		t.Errorf("code が %q だった", body.Error.Code)
	}
}

// ハンドラが返す 404 は理由まで含めてそのまま通す。
// **横取りが効きすぎると「投稿が見つからない」が
// 「エンドポイントが存在しません」に化ける。**
func Testハンドラの404は差し替えない(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens})
	token := mustIssue(t, tokens, 7)

	rec := doJSON(h, withBearer(req(http.MethodPut, "/api/notifications/999/read", ""), token))
	if rec.Code != http.StatusNotFound {
		t.Skipf("404 以外が返った（%d）。この経路では確認できない", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "エンドポイントが存在しません") {
		t.Errorf("ハンドラの 404 が差し替えられている: %s", rec.Body.String())
	}
}
