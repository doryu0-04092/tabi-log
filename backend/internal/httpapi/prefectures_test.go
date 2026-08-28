package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

func getPrefectures(t *testing.T, store PrefectureLister) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	newRouter(t, testDeps{prefectures: store}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prefectures", nil))
	return rec
}

func TestListPrefectures_取得した内容を返す(t *testing.T) {
	rec := getPrefectures(t, stubPrefectureLister{items: []domain.Prefecture{
		{Code: "01", Name: "北海道", NameKana: "ほっかいどう", Region: "北海道"},
		{Code: "13", Name: "東京都", NameKana: "とうきょうと", Region: "関東"},
	}})

	if rec.Code != http.StatusOK {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusOK, rec.Code)
	}

	var body struct {
		Data []struct {
			Code     string `json:"code"`
			Name     string `json:"name"`
			NameKana string `json:"nameKana"`
			Region   string `json:"region"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスの解析に失敗した: %v", err)
	}

	if len(body.Data) != 2 {
		t.Fatalf("件数: 期待 2, 実際 %d", len(body.Data))
	}
	// 先頭のゼロが落ちていないこと。数値として扱うと "01" が 1 になる。
	if body.Data[0].Code != "01" {
		t.Errorf("code: 期待 \"01\", 実際 %q", body.Data[0].Code)
	}
	if body.Data[1].Region != "関東" {
		t.Errorf("region: 期待 \"関東\", 実際 %q", body.Data[1].Region)
	}
}

// 内容が不変であることを利用者側に伝えられていること。
// あわせて、認証を要さない公開情報なので public を付けてよい。
func TestListPrefectures_キャッシュ可能であることを示す(t *testing.T) {
	rec := getPrefectures(t, stubPrefectureLister{items: []domain.Prefecture{}})

	got := rec.Header().Get("Cache-Control")
	if !strings.Contains(got, "public") || !strings.Contains(got, "max-age=") {
		t.Errorf("Cache-Control: public と max-age を期待, 実際 %q", got)
	}
}

// 0 件でも null ではなく [] を返すこと。
// null が混ざると、クライアント側で長さを取る前に毎回判定が要る。
func TestListPrefectures_0件でも空配列を返す(t *testing.T) {
	rec := getPrefectures(t, stubPrefectureLister{items: []domain.Prefecture{}})

	if body := strings.TrimSpace(rec.Body.String()); body != `{"data":[]}` {
		t.Errorf("本文: 期待 {\"data\":[]}, 実際 %s", body)
	}
}

func TestListPrefectures_取得に失敗したら500を返す(t *testing.T) {
	rec := getPrefectures(t, stubPrefectureLister{err: errors.New("Error 1146: Table 'tabilog.prefectures' doesn't exist")})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusInternalServerError, rec.Code)
	}

	// データベースの内部構造を利用者へ返していないこと。
	// テーブル名やスキーマ名が漏れると、攻撃の手がかりになる。
	if body := rec.Body.String(); strings.Contains(body, "tabilog") || strings.Contains(body, "Table") {
		t.Errorf("レスポンスに内部情報が含まれている: %s", body)
	}
}
