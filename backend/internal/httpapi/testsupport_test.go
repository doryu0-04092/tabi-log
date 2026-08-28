package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"

	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret-only-used-in-unit-tests"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testTokens(t *testing.T) *auth.JWTService {
	t.Helper()
	key := auth.SigningKey{ID: "v1", Method: jwt.SigningMethodHS256, Secret: []byte(testJWTSecret)}
	s, err := auth.NewJWTService(key, []auth.SigningKey{key}, 15*time.Minute)
	if err != nil {
		t.Fatalf("トークンサービスの作成に失敗した: %v", err)
	}
	return s
}

// stubPinger は疎通確認の結果を固定で返す。
type stubPinger struct{ err error }

func (s stubPinger) PingContext(context.Context) error { return s.err }

// stubPrefectureLister は固定の結果を返す。
type stubPrefectureLister struct {
	items []domain.Prefecture
	err   error
}

func (s stubPrefectureLister) List(context.Context) ([]domain.Prefecture, error) {
	return s.items, s.err
}

// testDeps はテスト用の依存一式を組み立てる。
type testDeps struct {
	pingErr     error
	prefectures PrefectureLister
	auth        AuthRepository
	tokens      *auth.JWTService
}

func newRouter(t *testing.T, d testDeps) http.Handler {
	t.Helper()

	if d.tokens == nil {
		d.tokens = testTokens(t)
	}
	if d.prefectures == nil {
		d.prefectures = stubPrefectureLister{items: []domain.Prefecture{}}
	}
	if d.auth == nil {
		d.auth = &stubAuthRepo{}
	}

	deps := Deps{
		DB:            stubPinger{err: d.pingErr},
		Prefectures:   d.prefectures,
		Auth:          d.auth,
		TokenIssuer:   d.tokens,
		TokenVerifier: d.tokens,
		AuthOptions: AuthOptions{
			RefreshTTL:   7 * 24 * time.Hour,
			RefreshGrace: 10 * time.Second,
			CookieSecure: false,
		},
		LoginAttemptLimit:  10,
		LoginAttemptWindow: 5 * time.Minute,
		Logger:             discardLogger(),
	}

	return NewRouter(deps)
}

// stubAuthRepo は AuthRepository の差し替え可能な実装。
//
// 各関数フィールドを設定しなければ、素直な既定動作をする。
type stubAuthRepo struct {
	users        map[string]domain.Credentials // email -> credentials
	byID         map[uint64]domain.User
	createErr    error
	createdUser  domain.User
	rotateUserID uint64
	rotateErr    error
	revokedHash  string
	savedTokens  []string
	findByIDErr  error
}

func (s *stubAuthRepo) CreateUser(_ context.Context, handle, email, passwordHash, displayName string) (domain.User, error) {
	if s.createErr != nil {
		return domain.User{}, s.createErr
	}
	u := domain.User{ID: 1, Handle: handle, Email: email, DisplayName: displayName}
	if s.createdUser.ID != 0 {
		u = s.createdUser
	}
	if s.byID == nil {
		s.byID = map[uint64]domain.User{}
	}
	s.byID[u.ID] = u
	_ = passwordHash
	return u, nil
}

func (s *stubAuthRepo) FindCredentialsByEmail(_ context.Context, email string) (domain.Credentials, error) {
	c, ok := s.users[email]
	if !ok {
		return domain.Credentials{}, errUserNotFoundForTest
	}
	return c, nil
}

func (s *stubAuthRepo) FindUserByID(_ context.Context, id uint64) (domain.User, error) {
	if s.findByIDErr != nil {
		return domain.User{}, s.findByIDErr
	}
	u, ok := s.byID[id]
	if !ok {
		return domain.User{}, errUserNotFoundForTest
	}
	return u, nil
}

func (s *stubAuthRepo) CreateRefreshToken(_ context.Context, _ uint64, hash string, _ time.Time) error {
	s.savedTokens = append(s.savedTokens, hash)
	return nil
}

func (s *stubAuthRepo) RotateRefreshToken(_ context.Context, _, _ string, _, _ time.Time, _ time.Duration) (uint64, error) {
	if s.rotateErr != nil {
		return 0, s.rotateErr
	}
	return s.rotateUserID, nil
}

func (s *stubAuthRepo) RevokeRefreshTokenByHash(_ context.Context, hash string, _ time.Time) error {
	s.revokedHash = hash
	return nil
}

// errUserNotFoundForTest は store.ErrUserNotFound と同じ扱いにするため、
// テスト側でも同一のエラー値を使う。
var errUserNotFoundForTest = store.ErrUserNotFound

// リクエストの組み立て補助。

func postJSON(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withCSRF(r *http.Request) *http.Request {
	r.Header.Set(csrfHeaderName, csrfHeaderValue)
	return r
}

func withBearer(r *http.Request, token string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func mustIssue(t *testing.T, s *auth.JWTService, userID uint64) string {
	t.Helper()
	token, _, err := s.Issue(userID, time.Now())
	if err != nil {
		t.Fatalf("トークンの発行に失敗した: %v", err)
	}
	return token
}

func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
