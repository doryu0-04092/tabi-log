// Package auth はアクセストークンの発行・検証と、パスワード・リフレッシュトークンの
// 取り扱いを担う。
//
// この2つの責務をここ1か所に閉じているのは、署名方式を変えるときの変更範囲を
// このパッケージと設定に限定するためである。上位層は TokenIssuer と
// TokenVerifier しか知らないので、HS256 から EdDSA へ移す際にも
// ハンドラもデータベースも触らずに済む。
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// アクセストークンの検証で返るエラー。呼び出し側は errors.Is で判別する。
var (
	// ErrTokenInvalid は署名・形式・種別のいずれかが正しくないことを表す。
	ErrTokenInvalid = errors.New("アクセストークンが不正である")
	// ErrTokenExpired は期限切れを表す。クライアントはリフレッシュを試みる。
	ErrTokenExpired = errors.New("アクセストークンの期限が切れている")
)

// tokenTypeAccess は typ クレームの値。
//
// 種別をクレームに入れておくと、別用途のトークン（将来のメール確認用など）が
// アクセストークンとして通ってしまう事故を防げる。
const tokenTypeAccess = "access"

// SigningKey は1つの署名鍵を表す。
type SigningKey struct {
	// ID は JWT ヘッダーの kid に入る。
	ID string
	// Method は署名アルゴリズム。**鍵と対で持つことが重要である**（後述）。
	Method jwt.SigningMethod
	Secret []byte
}

// Claims は検証を通ったアクセストークンの内容。
type Claims struct {
	UserID   uint64
	TokenID  string
	IssuedAt time.Time
	Expires  time.Time
}

// TokenIssuer はアクセストークンを発行する。
type TokenIssuer interface {
	Issue(userID uint64, now time.Time) (token string, expiresAt time.Time, err error)
}

// TokenVerifier はアクセストークンを検証する。
type TokenVerifier interface {
	Verify(token string) (Claims, error)
}

// JWTService はアクセストークンの発行と検証を行う。
type JWTService struct {
	// active は発行に使う鍵。
	active SigningKey
	// keys は検証に使う鍵を kid で引く。移行期間中は複数持つ。
	//
	// 発行用と検証用を分けているのが無停止で署名方式を変えられる理由である。
	// 「検証側に新しい鍵を足す → 発行を切り替える →
	// アクセストークンの寿命が過ぎたら旧鍵を消す」の3段階で移行できる。
	keys map[string]SigningKey
	ttl  time.Duration
	// newTokenID は jti を作る。テストで固定するために差し替えられる。
	newTokenID func() (string, error)
}

// NewJWTService を作る。keys には active を含めること。
func NewJWTService(active SigningKey, keys []SigningKey, ttl time.Duration) (*JWTService, error) {
	if len(active.Secret) == 0 {
		return nil, errors.New("署名鍵が空である")
	}
	if ttl <= 0 {
		return nil, errors.New("アクセストークンの有効期間が0以下である")
	}

	m := make(map[string]SigningKey, len(keys))
	for _, k := range keys {
		m[k.ID] = k
	}
	if _, ok := m[active.ID]; !ok {
		return nil, fmt.Errorf("発行用の鍵 %q が検証用の鍵に含まれていない", active.ID)
	}

	return &JWTService{active: active, keys: m, ttl: ttl, newTokenID: randomTokenID}, nil
}

// TTL はアクセストークンの有効期間を返す。
func (s *JWTService) TTL() time.Duration { return s.ttl }

// Issue はアクセストークンを発行する。
func (s *JWTService) Issue(userID uint64, now time.Time) (string, time.Time, error) {
	jti, err := s.newTokenID()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("トークンIDの生成に失敗した: %w", err)
	}

	expiresAt := now.Add(s.ttl)
	claims := jwt.MapClaims{
		"sub": strconv.FormatUint(userID, 10),
		"iat": jwt.NewNumericDate(now),
		"exp": jwt.NewNumericDate(expiresAt),
		"jti": jti,
		"typ": tokenTypeAccess,
	}

	token := jwt.NewWithClaims(s.active.Method, claims)
	// kid を最初から入れておくことで、鍵を増やしたときに
	// 既存のトークンも「どの鍵で検証すべきか」を自分で示せる。
	token.Header["kid"] = s.active.ID

	signed, err := token.SignedString(s.active.Secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("トークンの署名に失敗した: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify はアクセストークンを検証し、内容を返す。
func (s *JWTService) Verify(raw string) (Claims, error) {
	parsed, err := jwt.Parse(raw, s.keyFunc,
		// **許可するアルゴリズムを必ず明示する。**
		//
		// 指定しないと、トークンのヘッダーが自己申告した alg が採用される。
		// HMAC と公開鍵方式が混在した際に、攻撃者が公開鍵を HMAC の共有鍵として
		// 使い署名を偽造できる（alg confusion）。公開鍵は秘密ではないため
		// 誰でも入手できることが成立の理由である。
		//
		// 現在は HS256 のみだが、EdDSA へ移行する際もここに追加する形にし、
		// 「トークンの言い分ではなくサーバーが決める」構造を保つ。
		jwt.WithValidMethods(s.allowedMethods()),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, ErrTokenExpired
		}
		// 元のエラーも包む。呼び出し側の判定は errors.Is(err, ErrTokenInvalid) で
		// 足りるが、原因（署名不一致か形式不正か）をログで追えるようにしておく。
		return Claims{}, fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrTokenInvalid
	}

	// 種別が違うトークンをアクセストークンとして通さない。
	if typ, _ := claims["typ"].(string); typ != tokenTypeAccess {
		return Claims{}, fmt.Errorf("%w: 種別が %q である", ErrTokenInvalid, typ)
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return Claims{}, fmt.Errorf("%w: sub が無い", ErrTokenInvalid)
	}
	userID, err := strconv.ParseUint(sub, 10, 64)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: sub が利用者IDではない", ErrTokenInvalid)
	}

	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return Claims{}, fmt.Errorf("%w: exp が無い", ErrTokenInvalid)
	}
	iat, _ := claims.GetIssuedAt()
	issuedAt := time.Time{}
	if iat != nil {
		issuedAt = iat.Time
	}
	jti, _ := claims["jti"].(string)

	return Claims{UserID: userID, TokenID: jti, IssuedAt: issuedAt, Expires: exp.Time}, nil
}

// keyFunc は kid から検証鍵を引く。
//
// あわせて「その鍵に対応するアルゴリズムで署名されているか」も確認する。
// 鍵とアルゴリズムを対で持つのは、HS256 用の鍵で署名された値を
// 別方式として解釈させないためである。
func (s *JWTService) keyFunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("kid が無い")
	}
	key, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("kid %q に対応する鍵が無い", kid)
	}
	if token.Method.Alg() != key.Method.Alg() {
		return nil, fmt.Errorf("kid %q の鍵は %s 用だが %s で署名されている",
			kid, key.Method.Alg(), token.Method.Alg())
	}
	return key.Secret, nil
}

func (s *JWTService) allowedMethods() []string {
	seen := make(map[string]struct{}, len(s.keys))
	out := make([]string, 0, len(s.keys))
	for _, k := range s.keys {
		if _, dup := seen[k.Method.Alg()]; dup {
			continue
		}
		seen[k.Method.Alg()] = struct{}{}
		out = append(out, k.Method.Alg())
	}
	return out
}
