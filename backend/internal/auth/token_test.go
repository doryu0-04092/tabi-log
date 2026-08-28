package auth

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-not-used-anywhere-else"

func newTestService(t *testing.T) *JWTService {
	t.Helper()
	key := SigningKey{ID: "v1", Method: jwt.SigningMethodHS256, Secret: []byte(testSecret)}
	s, err := NewJWTService(key, []SigningKey{key}, 15*time.Minute)
	if err != nil {
		t.Fatalf("JWTService の作成に失敗した: %v", err)
	}
	return s
}

func TestIssueVerify_発行したトークンを検証できる(t *testing.T) {
	s := newTestService(t)
	now := time.Now()

	token, expiresAt, err := s.Issue(42, now)
	if err != nil {
		t.Fatalf("発行に失敗した: %v", err)
	}

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("検証に失敗した: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID: 期待 42, 実際 %d", claims.UserID)
	}
	if claims.TokenID == "" {
		t.Error("jti が空である")
	}
	if !expiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Errorf("有効期限: 期待 %v, 実際 %v", now.Add(15*time.Minute), expiresAt)
	}
}

func TestIssue_毎回異なるjtiになる(t *testing.T) {
	s := newTestService(t)
	now := time.Now()

	a, _, err := s.Issue(1, now)
	if err != nil {
		t.Fatalf("発行に失敗した: %v", err)
	}
	b, _, err := s.Issue(1, now)
	if err != nil {
		t.Fatalf("発行に失敗した: %v", err)
	}

	ca, _ := s.Verify(a)
	cb, _ := s.Verify(b)
	if ca.TokenID == cb.TokenID {
		t.Error("同じ jti が2回発行された")
	}
}

func TestVerify_期限切れは専用のエラーになる(t *testing.T) {
	s := newTestService(t)

	// 期限切れであることをクライアントが判別できないと、
	// リフレッシュすべき場面で再ログインを求めてしまう。
	token, _, err := s.Issue(1, time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("発行に失敗した: %v", err)
	}

	_, err = s.Verify(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("期待 ErrTokenExpired, 実際 %v", err)
	}
}

func TestVerify_署名を改竄したら拒否する(t *testing.T) {
	s := newTestService(t)
	token, _, _ := s.Issue(1, time.Now())

	// 署名部分の末尾1文字を変える。
	tampered := token[:len(token)-1] + string(flipChar(token[len(token)-1]))

	if _, err := s.Verify(tampered); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("期待 ErrTokenInvalid, 実際 %v", err)
	}
}

// alg confusion 攻撃への防御。
//
// 許可アルゴリズムを明示していないと、トークンのヘッダーが自己申告した
// alg がそのまま採用される。alg=none を受け入れると**署名なしのトークンで
// 誰にでもなりすませる**。
func TestVerify_algがnoneのトークンを拒否する(t *testing.T) {
	s := newTestService(t)

	claims := jwt.MapClaims{
		"sub": "1",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"typ": tokenTypeAccess,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tok.Header["kid"] = "v1"
	unsigned, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("テスト用トークンの作成に失敗した: %v", err)
	}

	if _, err := s.Verify(unsigned); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("alg=none のトークンが通ってしまった: err=%v", err)
	}
}

// 未知の kid を拒否できないと、鍵の入れ替え中に古い鍵で作られたトークンや、
// 存在しない鍵を指すトークンの扱いが曖昧になる。
func TestVerify_知らないkidを拒否する(t *testing.T) {
	s := newTestService(t)

	claims := jwt.MapClaims{
		"sub": "1",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"typ": tokenTypeAccess,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = "unknown-key"
	signed, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("テスト用トークンの作成に失敗した: %v", err)
	}

	if _, err := s.Verify(signed); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("未知の kid が通ってしまった: err=%v", err)
	}
}

func TestVerify_kidが無いトークンを拒否する(t *testing.T) {
	s := newTestService(t)

	claims := jwt.MapClaims{
		"sub": "1",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"typ": tokenTypeAccess,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("テスト用トークンの作成に失敗した: %v", err)
	}

	if _, err := s.Verify(signed); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("kid の無いトークンが通ってしまった: err=%v", err)
	}
}

// 種別を見ていないと、別用途で発行したトークンがアクセストークンとして通る。
func TestVerify_種別が違うトークンを拒否する(t *testing.T) {
	s := newTestService(t)

	claims := jwt.MapClaims{
		"sub": "1",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"typ": "password_reset",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = "v1"
	signed, _ := tok.SignedString([]byte(testSecret))

	if _, err := s.Verify(signed); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("種別の違うトークンが通ってしまった: err=%v", err)
	}
}

func TestVerify_expが無いトークンを拒否する(t *testing.T) {
	s := newTestService(t)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "1", "typ": tokenTypeAccess})
	tok.Header["kid"] = "v1"
	signed, _ := tok.SignedString([]byte(testSecret))

	if _, err := s.Verify(signed); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("exp の無いトークンが通ってしまった: err=%v", err)
	}
}

// 検証鍵を複数持てることが、無停止で署名方式を移行できる根拠になる。
func TestVerify_旧鍵で署名されたトークンも検証できる(t *testing.T) {
	oldKey := SigningKey{ID: "v1", Method: jwt.SigningMethodHS256, Secret: []byte("old-secret-value")}
	newKey := SigningKey{ID: "v2", Method: jwt.SigningMethodHS256, Secret: []byte("new-secret-value")}

	// 旧鍵で発行する（移行前の状態）
	oldSvc, err := NewJWTService(oldKey, []SigningKey{oldKey}, time.Hour)
	if err != nil {
		t.Fatalf("作成に失敗した: %v", err)
	}
	issuedWithOld, _, _ := oldSvc.Issue(7, time.Now())

	// 発行は新鍵に切り替えたが、検証は両方受け付ける（移行中の状態）
	migrating, err := NewJWTService(newKey, []SigningKey{oldKey, newKey}, time.Hour)
	if err != nil {
		t.Fatalf("作成に失敗した: %v", err)
	}

	claims, err := migrating.Verify(issuedWithOld)
	if err != nil {
		t.Fatalf("旧鍵のトークンが検証できない: %v", err)
	}
	if claims.UserID != 7 {
		t.Errorf("UserID: 期待 7, 実際 %d", claims.UserID)
	}

	// 新鍵で発行したものも当然通る
	issuedWithNew, _, _ := migrating.Issue(7, time.Now())
	if _, err := migrating.Verify(issuedWithNew); err != nil {
		t.Fatalf("新鍵のトークンが検証できない: %v", err)
	}
}

func TestNewJWTService_発行鍵が検証鍵に含まれないと作れない(t *testing.T) {
	active := SigningKey{ID: "v2", Method: jwt.SigningMethodHS256, Secret: []byte("s")}
	other := SigningKey{ID: "v1", Method: jwt.SigningMethodHS256, Secret: []byte("s")}

	// 自分で発行したトークンを自分で検証できない設定は、起動時に弾く。
	if _, err := NewJWTService(active, []SigningKey{other}, time.Hour); err == nil {
		t.Fatal("不整合な設定が受け入れられてしまった")
	}
}

func TestVerify_subが利用者IDでないトークンを拒否する(t *testing.T) {
	s := newTestService(t)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "not-a-number",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"typ": tokenTypeAccess,
	})
	tok.Header["kid"] = "v1"
	signed, _ := tok.SignedString([]byte(testSecret))

	if _, err := s.Verify(signed); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("sub が不正なトークンが通ってしまった: err=%v", err)
	}
}

// 大きな利用者IDが int64 経由で壊れないこと。
// BIGINT UNSIGNED の上半分の値が sub に入っても往復できる必要がある。
func TestIssueVerify_大きな利用者IDでも往復できる(t *testing.T) {
	s := newTestService(t)
	const big = uint64(1) << 63

	token, _, err := s.Issue(big, time.Now())
	if err != nil {
		t.Fatalf("発行に失敗した: %v", err)
	}
	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("検証に失敗した: %v", err)
	}
	if claims.UserID != big {
		t.Errorf("UserID: 期待 %d, 実際 %d", big, claims.UserID)
	}
	if got := strconv.FormatUint(claims.UserID, 10); !strings.Contains(token, "") || got == "" {
		t.Error("利用者IDの復元に失敗した")
	}
}

func flipChar(c byte) byte {
	if c == 'A' {
		return 'B'
	}
	return 'A'
}
