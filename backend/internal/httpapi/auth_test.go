package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func do(t *testing.T, router http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスの解析に失敗した: %v (本文: %s)", err, rec.Body.String())
	}
	return body.Error.Code
}

// ---------------------------------------------------------------------------
// サインアップ
// ---------------------------------------------------------------------------

func TestSignup_作成してセッションを開始する(t *testing.T) {
	repo := &stubAuthRepo{}
	router := newRouter(t, testDeps{auth: repo})

	rec := do(t, router, postJSON("/api/auth/signup",
		`{"email":"a@example.com","handle":"traveler_01","displayName":"たびびと","password":"password123"}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("ステータス: 期待 %d, 実際 %d (本文: %s)", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			AccessToken string `json:"accessToken"`
			ExpiresIn   int    `json:"expiresIn"`
			User        struct {
				Handle      string `json:"handle"`
				DisplayName string `json:"displayName"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスの解析に失敗した: %v", err)
	}
	if body.Data.AccessToken == "" {
		t.Error("アクセストークンが空である")
	}
	if body.Data.ExpiresIn <= 0 {
		t.Errorf("expiresIn: 期待 正の値, 実際 %d", body.Data.ExpiresIn)
	}
	if body.Data.User.Handle != "traveler_01" {
		t.Errorf("handle: 実際 %q", body.Data.User.Handle)
	}

	// リフレッシュトークンは Cookie で渡す。
	c := cookieByName(rec, refreshCookieName)
	if c == nil {
		t.Fatal("リフレッシュトークンの Cookie が設定されていない")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly が付いていない（JavaScript から読めてしまう）")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Error("SameSite=Strict が付いていない")
	}
	if c.Path != refreshCookiePath {
		t.Errorf("Path: 期待 %q, 実際 %q", refreshCookiePath, c.Path)
	}

	// 保存されるのはハッシュであり、Cookie の値そのものではない。
	if len(repo.savedTokens) != 1 {
		t.Fatalf("保存されたトークン数: 期待 1, 実際 %d", len(repo.savedTokens))
	}
	if repo.savedTokens[0] == c.Value {
		t.Error("リフレッシュトークンが平文で保存されている")
	}
	if repo.savedTokens[0] != auth.HashRefreshToken(c.Value) {
		t.Error("保存された値が Cookie のハッシュと一致しない")
	}
}

// レスポンスにパスワードやメールアドレスが混ざらないこと。
// gen.User にそれらの項目が無いことをテストでも押さえておく。
func TestSignup_レスポンスに秘密を含めない(t *testing.T) {
	router := newRouter(t, testDeps{auth: &stubAuthRepo{}})

	rec := do(t, router, postJSON("/api/auth/signup",
		`{"email":"secret@example.com","handle":"traveler_01","displayName":"たびびと","password":"password123"}`))

	body := rec.Body.String()
	for _, forbidden := range []string{"password123", "secret@example.com", "passwordHash"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("レスポンスに %q が含まれている: %s", forbidden, body)
		}
	}
}

func TestSignup_入力の検証(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"メールアドレスの形式が不正", `{"email":"not-an-email","handle":"aaa","displayName":"x","password":"password123"}`},
		{"ハンドルが短すぎる", `{"email":"a@example.com","handle":"ab","displayName":"x","password":"password123"}`},
		{"ハンドルに記号が入る", `{"email":"a@example.com","handle":"has-hyphen","displayName":"x","password":"password123"}`},
		{"表示名が空", `{"email":"a@example.com","handle":"aaa","displayName":"","password":"password123"}`},
		{"パスワードが短い", `{"email":"a@example.com","handle":"aaa","displayName":"x","password":"short"}`},
		// bcrypt は 72 バイトを超える入力を切り捨てる。黙って受け入れない。
		{"パスワードが72バイト超", `{"email":"a@example.com","handle":"aaa","displayName":"x","password":"` + strings.Repeat("a", 73) + `"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(t, testDeps{auth: &stubAuthRepo{}})
			rec := do(t, router, postJSON("/api/auth/signup", tt.body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("ステータス: 期待 %d, 実際 %d (本文: %s)", http.StatusBadRequest, rec.Code, rec.Body.String())
			}
			if code := errorCode(t, rec); code != "validation_error" {
				t.Errorf("code: 期待 validation_error, 実際 %q", code)
			}
		})
	}
}

func TestSignup_重複を409で返す(t *testing.T) {
	for _, tt := range []struct {
		name     string
		storeErr error
		wantCode string
	}{
		{"メールアドレスの重複", store.ErrEmailTaken, "email_taken"},
		{"ハンドルの重複", store.ErrHandleTaken, "handle_taken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(t, testDeps{auth: &stubAuthRepo{createErr: tt.storeErr}})
			rec := do(t, router, postJSON("/api/auth/signup",
				`{"email":"a@example.com","handle":"traveler_01","displayName":"x","password":"password123"}`))

			if rec.Code != http.StatusConflict {
				t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusConflict, rec.Code)
			}
			if code := errorCode(t, rec); code != tt.wantCode {
				t.Errorf("code: 期待 %q, 実際 %q", tt.wantCode, code)
			}
		})
	}
}

// 仕様に無いフィールドを黙って無視すると、
// 書き間違えたフィールドが「設定したのに効かない」状態になる。
func TestSignup_未知のフィールドを拒否する(t *testing.T) {
	router := newRouter(t, testDeps{auth: &stubAuthRepo{}})

	rec := do(t, router, postJSON("/api/auth/signup",
		`{"email":"a@example.com","handle":"aaa","displayName":"x","password":"password123","isAdmin":true}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusBadRequest, rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ログイン
// ---------------------------------------------------------------------------

func loginRepo(t *testing.T) *stubAuthRepo {
	t.Helper()
	hash, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("ハッシュ化に失敗した: %v", err)
	}
	user := domain.User{ID: 7, Handle: "traveler_01", Email: "a@example.com", DisplayName: "たびびと"}
	return &stubAuthRepo{
		users: map[string]domain.Credentials{
			"a@example.com": {User: user, PasswordHash: hash},
		},
		byID: map[uint64]domain.User{7: user},
	}
}

func TestLogin_正しい資格情報でセッションを開始する(t *testing.T) {
	router := newRouter(t, testDeps{auth: loginRepo(t)})

	rec := do(t, router, postJSON("/api/auth/login", `{"email":"a@example.com","password":"correct-password"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("ステータス: 期待 %d, 実際 %d (本文: %s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if cookieByName(rec, refreshCookieName) == nil {
		t.Error("リフレッシュトークンの Cookie が設定されていない")
	}
}

// **失敗の理由を区別しないこと。**
// 「登録されていない」と「パスワードが違う」で応答が変わると、
// どのメールアドレスが登録済みかを総当たりで調べられる。
func TestLogin_失敗の理由を区別しない(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"存在しないメールアドレス", `{"email":"nobody@example.com","password":"correct-password"}`},
		{"パスワードが違う", `{"email":"a@example.com","password":"wrong-password"}`},
	}

	var codes []string
	var bodies []string
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(t, testDeps{auth: loginRepo(t)})
			rec := do(t, router, postJSON("/api/auth/login", tt.body))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
			}
			codes = append(codes, errorCode(t, rec))
			bodies = append(bodies, rec.Body.String())
		})
	}

	if len(codes) == 2 && (codes[0] != codes[1] || bodies[0] != bodies[1]) {
		t.Errorf("失敗の応答が区別できてしまう: %q vs %q", bodies[0], bodies[1])
	}
	for _, c := range codes {
		if c != "invalid_credentials" {
			t.Errorf("code: 期待 invalid_credentials, 実際 %q", c)
		}
	}
}

func TestLogin_試行回数の上限を超えたら429を返す(t *testing.T) {
	router := newRouter(t, testDeps{auth: loginRepo(t)})

	// 上限は 10 回。11 回目で 429 になる。
	var last *httptest.ResponseRecorder
	for range 11 {
		last = do(t, router, postJSON("/api/auth/login", `{"email":"a@example.com","password":"wrong"}`))
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusTooManyRequests, last.Code)
	}
	if code := errorCode(t, last); code != "rate_limited" {
		t.Errorf("code: 期待 rate_limited, 実際 %q", code)
	}
}

// ---------------------------------------------------------------------------
// トークンの再発行
// ---------------------------------------------------------------------------

func refreshRequest(cookieValue string) *http.Request {
	r := withCSRF(postJSON("/api/auth/refresh", ""))
	if cookieValue != "" {
		r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: cookieValue})
	}
	return r
}

func TestRefresh_新しいトークンを発行しCookieを更新する(t *testing.T) {
	repo := loginRepo(t)
	repo.rotateUserID = 7
	router := newRouter(t, testDeps{auth: repo})

	rec := do(t, router, refreshRequest("old-refresh-token"))

	if rec.Code != http.StatusOK {
		t.Fatalf("ステータス: 期待 %d, 実際 %d (本文: %s)", http.StatusOK, rec.Code, rec.Body.String())
	}

	c := cookieByName(rec, refreshCookieName)
	if c == nil {
		t.Fatal("Cookie が設定されていない")
	}
	// ローテーションされていること。同じ値が返ると使い回しになる。
	if c.Value == "old-refresh-token" {
		t.Error("リフレッシュトークンがローテーションされていない")
	}
}

// CSRF 対策。Cookie で認証するエンドポイントは、
// カスタムヘッダーが無いリクエストを受け付けてはならない。
func TestRefresh_CSRFヘッダーが無ければ拒否する(t *testing.T) {
	router := newRouter(t, testDeps{auth: loginRepo(t)})

	r := postJSON("/api/auth/refresh", "")
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "some-token"})
	rec := do(t, router, r)

	if rec.Code == http.StatusOK {
		t.Fatal("CSRF ヘッダーが無いのに成功した")
	}
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Fatalf("ステータス: 期待 400 か 403, 実際 %d", rec.Code)
	}
	// 生成コードの既定は text/plain を返す。JSON に揃えていること。
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: 期待 JSON, 実際 %q", ct)
	}
}

func TestRefresh_CSRFヘッダーの値が違えば拒否する(t *testing.T) {
	router := newRouter(t, testDeps{auth: loginRepo(t)})

	r := postJSON("/api/auth/refresh", "")
	r.Header.Set(csrfHeaderName, "something-else")
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "some-token"})
	rec := do(t, router, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusForbidden, rec.Code)
	}
	if code := errorCode(t, rec); code != "csrf_rejected" {
		t.Errorf("code: 期待 csrf_rejected, 実際 %q", code)
	}
}

func TestRefresh_Cookieが無ければ401を返す(t *testing.T) {
	router := newRouter(t, testDeps{auth: loginRepo(t)})

	rec := do(t, router, refreshRequest(""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
	}
}

// 盗用を検知した場合、利用者には「全セッションを終了した」と伝わる必要がある。
// 単なる 401 と同じ扱いだと、なぜ再ログインが要るのか説明できない。
func TestRefresh_盗用検知は専用のコードとCookie削除を伴う(t *testing.T) {
	repo := loginRepo(t)
	repo.rotateErr = store.ErrRefreshTokenReused
	router := newRouter(t, testDeps{auth: repo})

	rec := do(t, router, refreshRequest("stolen-token"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
	}
	if code := errorCode(t, rec); code != "token_reuse_detected" {
		t.Errorf("code: 期待 token_reuse_detected, 実際 %q", code)
	}
	c := cookieByName(rec, refreshCookieName)
	if c == nil || c.MaxAge >= 0 {
		t.Error("Cookie が削除されていない")
	}
}

func TestRefresh_期限切れならCookieを削除して401を返す(t *testing.T) {
	repo := loginRepo(t)
	repo.rotateErr = store.ErrRefreshTokenExpired
	router := newRouter(t, testDeps{auth: repo})

	rec := do(t, router, refreshRequest("expired-token"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
	}
	if c := cookieByName(rec, refreshCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("Cookie が削除されていない")
	}
}

// ---------------------------------------------------------------------------
// ログアウト
// ---------------------------------------------------------------------------

func TestLogout_トークンを失効させCookieを削除する(t *testing.T) {
	repo := loginRepo(t)
	router := newRouter(t, testDeps{auth: repo})

	r := withCSRF(postJSON("/api/auth/logout", ""))
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "my-token"})
	rec := do(t, router, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusNoContent, rec.Code)
	}
	// 平文ではなくハッシュで失効させていること。
	if repo.revokedHash != auth.HashRefreshToken("my-token") {
		t.Errorf("失効に使われた値がハッシュでない: %q", repo.revokedHash)
	}
	if c := cookieByName(rec, refreshCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("Cookie が削除されていない")
	}
}

// 既にログアウトしている状態でのログアウトは失敗ではない。
func TestLogout_Cookieが無くても成功する(t *testing.T) {
	router := newRouter(t, testDeps{auth: loginRepo(t)})

	rec := do(t, router, withCSRF(postJSON("/api/auth/logout", "")))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusNoContent, rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ログイン中の利用者
// ---------------------------------------------------------------------------

func TestGetMe_トークンが指す利用者を返す(t *testing.T) {
	tokens := testTokens(t)
	router := newRouter(t, testDeps{auth: loginRepo(t), tokens: tokens})

	rec := do(t, router, withBearer(httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), mustIssue(t, tokens, 7)))

	if rec.Code != http.StatusOK {
		t.Fatalf("ステータス: 期待 %d, 実際 %d (本文: %s)", http.StatusOK, rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			Handle string `json:"handle"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスの解析に失敗した: %v", err)
	}
	if body.Data.Handle != "traveler_01" {
		t.Errorf("handle: 実際 %q", body.Data.Handle)
	}
}

func TestGetMe_メールアドレスを返さない(t *testing.T) {
	tokens := testTokens(t)
	router := newRouter(t, testDeps{auth: loginRepo(t), tokens: tokens})

	rec := do(t, router, withBearer(httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), mustIssue(t, tokens, 7)))

	// 公開してよい情報だけを返す。他人のプロフィールでも同じ型を使うため、
	// ここにメールアドレスが混ざると全利用者のアドレスが漏れる経路になる。
	if strings.Contains(rec.Body.String(), "a@example.com") {
		t.Errorf("レスポンスにメールアドレスが含まれている: %s", rec.Body.String())
	}
}

/*
古いコストで作られたハッシュは、ログイン成功時に付け直す。

**bcrypt はコストをハッシュ自体に記録する。** そのため設定を変えても
既存の利用者は古いコストのまま検証され、**その人だけが取り残される。**

2026-08-31 に実際にそうなった。コストを 12 → 10 に下げたところ、
新規の利用者は 222ms で入れるのに、既存の利用者は 1100ms のままだった。
パスワードを変えるまで直らない。

`perf/README.md` の「smoke の p95 が 1.11 秒だった理由」。
*/
func Test古いコストのハッシュはログイン時に付け直す(t *testing.T) {
	// 以前の設定（12）で作られたハッシュを持つ利用者。
	old, err := bcrypt.GenerateFromPassword([]byte("correct horse battery"), 12)
	if err != nil {
		t.Fatalf("ハッシュを作れない: %v", err)
	}
	user := domain.User{ID: 7, Handle: "traveler_01", Email: "a@example.com", DisplayName: "たびびと"}
	repo := &stubAuthRepo{
		users: map[string]domain.Credentials{
			"a@example.com": {User: user, PasswordHash: string(old)},
		},
		byID: map[uint64]domain.User{7: user},
	}
	h := newRouter(t, testDeps{auth: repo})

	rec := doJSON(h, req(http.MethodPost, "/api/auth/login",
		`{"email":"a@example.com","password":"correct horse battery"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("ログインが %d で返った: %s", rec.Code, rec.Body.String())
	}

	got, ok := repo.rehashed[7]
	if !ok {
		t.Fatal("付け直していない。設定を変えても既存の利用者だけ取り残される")
	}
	cost, err := bcrypt.Cost([]byte(got))
	if err != nil {
		t.Fatalf("付け直したハッシュが読めない: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("付け直したコストが %d。%d を期待した", cost, bcrypt.DefaultCost)
	}
}

// 現在のコストなら触らない。**毎回書き込むと無駄な更新が増える。**
func Test現在のコストのハッシュは付け直さない(t *testing.T) {
	repo := loginRepo(t)
	h := newRouter(t, testDeps{auth: repo})

	rec := doJSON(h, req(http.MethodPost, "/api/auth/login",
		`{"email":"a@example.com","password":"correct-password"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("ログインが %d で返った", rec.Code)
	}
	if len(repo.rehashed) != 0 {
		t.Errorf("付け直す必要がないのに書き込んだ: %v", repo.rehashed)
	}
}

// **付け直せなくてもログインは成功させる。**
// 認証は成立しており、利用者から見て壊れてはいない。
func Test付け直せなくてもログインは成功する(t *testing.T) {
	old, err := bcrypt.GenerateFromPassword([]byte("correct horse battery"), 12)
	if err != nil {
		t.Fatalf("ハッシュを作れない: %v", err)
	}
	user := domain.User{ID: 7, Handle: "traveler_01", Email: "a@example.com", DisplayName: "たびびと"}
	repo := &stubAuthRepo{
		users:     map[string]domain.Credentials{"a@example.com": {User: user, PasswordHash: string(old)}},
		byID:      map[uint64]domain.User{7: user},
		rehashErr: errors.New("DB に届かない"),
	}
	h := newRouter(t, testDeps{auth: repo})

	rec := doJSON(h, req(http.MethodPost, "/api/auth/login",
		`{"email":"a@example.com","password":"correct horse battery"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("ログインが %d で返った。付け直しの失敗で認証を落としてはいけない", rec.Code)
	}
}
