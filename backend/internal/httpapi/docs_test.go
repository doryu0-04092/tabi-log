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
