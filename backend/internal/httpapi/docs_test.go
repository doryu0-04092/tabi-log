package httpapi

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func Test仕様書は認証なしで読める(t *testing.T) {
	h := newRouter(t, testDeps{})

	for _, tc := range []struct {
		path        string
		contentType string
	}{
		{"/api/docs", "text/html"},
		{"/api/openapi.yaml", "application/yaml"},
	} {
		rec := doJSON(h, req(http.MethodGet, tc.path, ""))
		if rec.Code != http.StatusOK {
			t.Errorf("%s が %d を返した。200 を期待した", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tc.contentType) {
			t.Errorf("%s の Content-Type が %q。%q で始まるはず", tc.path, got, tc.contentType)
		}
	}
}

// **配っている仕様が docs/openapi.yaml と同じであること。**
//
// `go:embed` は package の外を参照できないため写しを置いており、
// go generate が置き直す。CI の「生成物の一致検証」でも見ているが、
// **写しが古いまま配られる**という間違い方は害が大きいので、
// テストでも直接確かめる。
func Test配っている仕様が正本と一致する(t *testing.T) {
	original, err := os.ReadFile("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("正本を読めない: %v", err)
	}
	if string(original) != string(openAPISpec) {
		t.Error("配っている仕様が docs/openapi.yaml と違う。`go generate ./...` を実行すること")
	}
}

/*
本番では仕様を読む画面を配らない。

このページは外部 CDN のスクリプトを同一オリジンで動かす。CDN 側が
改ざんされると、そこから `/api/auth/refresh` を呼べる。CSRF 用の
ヘッダーは固定文字列なので防げず、**閲覧中の利用者のアクセストークンが
取られうる。**

`integrity` は付けたが、**本物の利用者が本番で開く状況をそもそも
作らない**のが最も確実である。

`docs/audit-2026-08-31.md` H4。
*/
func Test本番では仕様を読む画面を配らない(t *testing.T) {
	h := newRouter(t, testDeps{isProduction: true})

	rec := doJSON(h, req(http.MethodGet, "/api/docs", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}

	// **仕様そのものは配り続ける。** 危険なのは外部スクリプトを
	// 同一オリジンで動かすことであって、仕様が読めることではない。
	spec := doJSON(h, req(http.MethodGet, "/api/openapi.yaml", ""))
	if spec.Code != http.StatusOK {
		t.Errorf("仕様が %d で返った。本番でも配り続けるべき", spec.Code)
	}
}

// 本番以外では今までどおり配る。**開発の手段を奪わない。**
func Test本番以外では仕様を読む画面を配る(t *testing.T) {
	h := newRouter(t, testDeps{})

	rec := doJSON(h, req(http.MethodGet, "/api/docs", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した", rec.Code)
	}
	// 完全性の検証が外れていないこと。**外れると改ざんに気づけない。**
	if !strings.Contains(rec.Body.String(), "integrity=") {
		t.Error("integrity が付いていない")
	}
}
