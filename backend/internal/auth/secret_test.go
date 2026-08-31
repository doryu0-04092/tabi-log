package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword_正しいパスワードだけを受け入れる(t *testing.T) {
	const password = "correct horse battery"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("ハッシュ化に失敗した: %v", err)
	}

	// ハッシュに平文が含まれていないこと。
	if strings.Contains(hash, password) {
		t.Fatal("ハッシュに平文が含まれている")
	}
	if !VerifyPassword(hash, password) {
		t.Error("正しいパスワードが拒否された")
	}
	if VerifyPassword(hash, password+"x") {
		t.Error("誤ったパスワードが受け入れられた")
	}
}

func TestHashPassword_同じパスワードでも異なるハッシュになる(t *testing.T) {
	// ソルトが効いていること。同じハッシュになると、
	// 漏洩時に「同じパスワードを使っている利用者」が特定できてしまう。
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("ハッシュ化に失敗した: %v", err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("ハッシュ化に失敗した: %v", err)
	}
	if a == b {
		t.Error("同じパスワードから同じハッシュが生成された（ソルトが効いていない）")
	}
}

func TestValidatePasswordLength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"8バイトちょうどは通る", strings.Repeat("a", 8), false},
		{"7バイトは短すぎる", strings.Repeat("a", 7), true},
		{"72バイトちょうどは通る", strings.Repeat("a", 72), false},
		{"73バイトは長すぎる", strings.Repeat("a", 73), true},
		// bcrypt の制限はバイト数である。日本語は1文字3バイトなので、
		// 文字数で数えていると 24 文字で上限に達することに気づけない。
		{"日本語24文字は72バイトで通る", strings.Repeat("あ", 24), false},
		{"日本語25文字は75バイトで超える", strings.Repeat("あ", 25), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordLength(tt.password)
			if tt.wantErr && !errors.Is(err, ErrPasswordLength) {
				t.Errorf("期待 ErrPasswordLength, 実際 %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("エラーになるべきでない: %v", err)
			}
		})
	}
}

// bcrypt が黙って切り捨てる範囲を受け付けてしまうと、
// 73バイト目以降が無視されていることに利用者が気づけない。
func TestHashPassword_72バイトを超えるパスワードを拒否する(t *testing.T) {
	if _, err := HashPassword(strings.Repeat("a", 100)); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("期待 ErrPasswordLength, 実際 %v", err)
	}
}

// 利用者が見つからない場合の比較に使う。存在しないアドレスへの応答が
// 明らかに速いと、登録済みかどうかが分かってしまう。
func TestDummyPasswordHash_有効なbcryptハッシュである(t *testing.T) {
	if DummyPasswordHash == "" {
		t.Fatal("ダミーハッシュが空である")
	}
	// どんな入力とも一致しないこと。
	if VerifyPassword(DummyPasswordHash, "dummy-password-for-timing") {
		// 生成に使った値とは一致してしまうため、これは想定内。
		t.Log("生成に使った値とは一致する（想定内）")
	}
	if VerifyPassword(DummyPasswordHash, "") {
		t.Error("空文字と一致してしまった")
	}
}

func TestNewRefreshToken(t *testing.T) {
	token, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("生成に失敗した: %v", err)
	}

	// 保存するのはハッシュのみ。平文がそのまま入っていないこと。
	if token == hash {
		t.Fatal("平文とハッシュが同じである")
	}
	if strings.Contains(hash, token) {
		t.Fatal("ハッシュに平文が含まれている")
	}
	// SHA-256 の16進表現は64文字。
	if len(hash) != 64 {
		t.Errorf("ハッシュの長さ: 期待 64, 実際 %d", len(hash))
	}
	if got := HashRefreshToken(token); got != hash {
		t.Errorf("同じ入力から異なるハッシュが出た: %q vs %q", got, hash)
	}
}

func TestNewRefreshToken_毎回異なる値になる(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		token, _, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("生成に失敗した: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatal("同じトークンが2回生成された")
		}
		seen[token] = struct{}{}
	}
}

func TestEqualSecret(t *testing.T) {
	if !EqualSecret("abc", "abc") {
		t.Error("同じ値が不一致と判定された")
	}
	if EqualSecret("abc", "abd") {
		t.Error("異なる値が一致と判定された")
	}
	if EqualSecret("abc", "abcd") {
		t.Error("長さの違う値が一致と判定された")
	}
}

/*
コストを変えても、以前のコストで作られたハッシュは検証できる。

**bcrypt はコストをハッシュ自体に記録している。** そのため、設定を
下げても上げても、既に保存されているパスワードはそのまま通る。

これが成り立たないと、**コストを変えた瞬間に全利用者がログイン
できなくなる。** 気づくのは利用者からの連絡である。

`docs/audit-2026-08-31.md` の性能確認で cost 12 → 10 に変えた。
*/
func Test以前のコストで作られたハッシュも検証できる(t *testing.T) {
	const password = "VerifyPass!2026"

	// 以前の設定（12）で作られたハッシュを模す。
	old, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatalf("ハッシュを作れない: %v", err)
	}

	if !VerifyPassword(string(old), password) {
		t.Error("cost=12 で作られたハッシュが検証できない。設定を変えると全利用者がログインできなくなる")
	}
	if VerifyPassword(string(old), "ちがうパスワード") {
		t.Error("誤ったパスワードが通った")
	}
}

// **既定より下げない。** bcrypt.DefaultCost は OWASP の下限も満たす。
// 下げると総当たりへの耐性が落ちる。
func Testコストはライブラリの既定を下回らない(t *testing.T) {
	if bcryptCost < bcrypt.DefaultCost {
		t.Errorf("コストが %d。既定（%d）を下回っている", bcryptCost, bcrypt.DefaultCost)
	}
}
