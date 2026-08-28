package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// livez は依存先を見ないため、データベースが落ちていても 200 を返さなければならない。
// これが崩れると、DB の一時的な不調で全タスクが置き換えられる状態に戻る。
func TestLivez_DBが落ちていても200を返す(t *testing.T) {
	router := newRouter(t, testDeps{pingErr: errors.New("connection refused")})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/livez", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusOK, rec.Code)
	}
	if got := rec.Header().Get(requestIDHeader); got == "" {
		t.Errorf("%s ヘッダーが空である", requestIDHeader)
	}
}

func TestReadyz_DBが正常なら200を返す(t *testing.T) {
	router := newRouter(t, testDeps{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusOK, rec.Code)
	}

	var body struct {
		Data struct {
			Status   string `json:"status"`
			Database string `json:"database"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスの解析に失敗した: %v", err)
	}
	if body.Data.Database != "ok" {
		t.Errorf("database: 期待 \"ok\", 実際 %q", body.Data.Database)
	}
}

func TestReadyz_DBが落ちていたら503を返す(t *testing.T) {
	router := newRouter(t, testDeps{pingErr: errors.New("connection refused")})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusServiceUnavailable, rec.Code)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスの解析に失敗した: %v", err)
	}
	if body.Error.Code != "dependency_unavailable" {
		t.Errorf("error.code: 期待 \"dependency_unavailable\", 実際 %q", body.Error.Code)
	}
}

// 未定義のパスで HTML が返ると、JSON を期待するクライアントが解釈に失敗する。
//
// 認証済みの場合に 404 を返す。未認証の場合は後述のとおり 401 になる。
func TestNotFound_認証済みならJSONの404を返す(t *testing.T) {
	tokens := testTokens(t)
	router := newRouter(t, testDeps{tokens: tokens})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, withBearer(httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil),
		mustIssue(t, tokens, 1)))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusNotFound, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: 期待 JSON, 実際 %q", ct)
	}
}

// 未認証で未定義のパスを叩くと 401 になる。
//
// **これは意図した挙動である。** 認証ミドルウェアが mux より外側にあり、
// publicPaths に無いパスは「存在するかどうか」を判定する前に拒否する。
// 未認証の相手に、どのエンドポイントが存在するかを教えない。
func TestNotFound_未認証なら存在の有無を明かさず401を返す(t *testing.T) {
	router := newRouter(t, testDeps{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
	}
}

// panic がそのまま伝播するとサーバー全体が停止し、1件のバグで全利用者に影響する。
func TestWithRecovery_panicを500に変換する(t *testing.T) {
	handler := chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		WithRequestID,
		WithRecovery(discardLogger()),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusInternalServerError, rec.Code)
	}
	// 内部の詳細（panic の値やスタック）を利用者へ返していないこと。
	if body := rec.Body.String(); strings.Contains(body, "boom") {
		t.Errorf("レスポンスに内部情報が含まれている: %s", body)
	}
}
