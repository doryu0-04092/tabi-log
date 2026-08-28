package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost はパスワードのハッシュ化コスト。
//
// 既定の 10 より1段上げている。総当たりに要する時間は 2 のべき乗で増える一方、
// ログイン1回あたりの遅延は数十ミリ秒の増加に留まるためである。
const bcryptCost = 12

// maxPasswordBytes は受け付けるパスワードの最大バイト数。
//
// **bcrypt は 72 バイトを超える入力を切り捨てる。** これを知らずに
// 長いパスフレーズを受け付けると、73 バイト目以降が無視されているのに
// 利用者は「長いから安全」と信じることになる。
// 切り捨てて黙って受け入れるのではなく、明示的に拒否する。
const maxPasswordBytes = 72

// minPasswordBytes は受け付ける最小バイト数。
const minPasswordBytes = 8

// ErrPasswordLength は長さの要件を満たさないことを表す。
var ErrPasswordLength = errors.New("パスワードの長さが要件を満たしていない")

// HashPassword はパスワードをハッシュ化する。
func HashPassword(password string) (string, error) {
	if err := ValidatePasswordLength(password); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("パスワードのハッシュ化に失敗した: %w", err)
	}
	return string(h), nil
}

// ValidatePasswordLength は長さだけを検証する。
//
// **バイト数で数える。** 日本語のパスフレーズは1文字あたり3バイト程度になるため、
// 文字数で数えると bcrypt の 72 バイト制限を超えたことに気づけない。
func ValidatePasswordLength(password string) error {
	n := len(password)
	if n < minPasswordBytes {
		return fmt.Errorf("%w: %d バイトしかない（%d バイト以上が必要）",
			ErrPasswordLength, n, minPasswordBytes)
	}
	if n > maxPasswordBytes {
		return fmt.Errorf("%w: %d バイトある（%d バイトまで）",
			ErrPasswordLength, n, maxPasswordBytes)
	}
	return nil
}

// VerifyPassword はハッシュと平文が一致するかを返す。
//
// bcrypt.CompareHashAndPassword は一致しない場合にエラーを返すが、
// **理由の区別を呼び出し側へ渡さない**。ハッシュが壊れているのか
// パスワードが違うのかで応答を変えると、そこから情報が漏れるためである。
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// DummyPasswordHash は存在しない利用者に対して比較を行うためのハッシュ。
//
// **利用者が見つからないときに即座に返すと、応答時間の差で
// 「そのメールアドレスは登録されているか」が分かってしまう**（タイミング攻撃）。
// 見つからない場合もこのハッシュに対して bcrypt の比較を行い、
// 処理時間を揃える。
var DummyPasswordHash = mustHashDummy()

func mustHashDummy() string {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcryptCost)
	if err != nil {
		panic("ダミーハッシュの生成に失敗した: " + err.Error())
	}
	return string(h)
}

// refreshTokenBytes はリフレッシュトークンの乱数の長さ。
//
// 128 ビットで衝突・推測に対して十分だが、値そのものが資格情報であるため
// 余裕をみて 256 ビットにしている。
const refreshTokenBytes = 32

// NewRefreshToken は新しいリフレッシュトークンと、その保存用ハッシュを返す。
//
// 返り値の1つ目だけを利用者へ渡し、**2つ目だけをデータベースへ保存する**。
// 平文を保存しないため、データベースが漏洩してもトークンとしては使えない。
func NewRefreshToken() (token string, hash string, err error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("リフレッシュトークンの生成に失敗した: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken は保存・照合に使うハッシュを返す。
//
// パスワードと違い bcrypt を使わないのは、**元の値が 256 ビットの乱数であり
// 総当たりが成立しない**ためである。ここで意図的に遅いハッシュを使うと、
// リフレッシュのたびに数十ミリ秒を払うだけになる。
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// EqualSecret は2つの秘密値を定数時間で比較する。
//
// 通常の文字列比較は最初に違うバイトで打ち切るため、
// 一致した接頭辞の長さが処理時間に現れる。
func EqualSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// randomTokenID は JWT の jti に使う識別子を返す。
func randomTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
