package storage

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // CloudFront の署名は RSA-SHA1 と決まっている。選択の余地は無い
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

/*
CloudFront 経由で画像を配る。

**S3 の署名付き URL との決定的な違いは、URL が変わらないことである。**

署名付き URL は呼ぶたびに署名と時刻が変わるため、**同じ画像でも毎回別の URL** に
なる。その結果、エッジのキャッシュも、ブラウザのキャッシュも一度も当たらない。
画像が主役のサービスでは、これが転送量のほとんどを占める経路で起きていた。

こちらは URL を `https://<配信ドメイン>/<鍵>` に固定し、
**読む権利は Cookie の側に持たせる。** URL が変わらないので、
エッジにもブラウザにも載る。

---

**配信するのは variants/ 配下だけである。**

`originals/` にはアップロードされたままの画像があり、**EXIF が残っている**
（GPS 座標を含む）。EXIF を落とすのは変換の工程であり、
その成果物が variants/ である。ここを取り違えると、
位置情報を県単位に絞るという判断が画像経由で無効になる。
*/

// CDNSigner は CloudFront の配信ドメインと署名鍵を持つ。
type CDNSigner struct {
	domain    string
	keyPairID string
	key       *rsa.PrivateKey

	// resource は Cookie が許す範囲。variants/ 配下に限る。
	resource string
}

// CDNAllowedPrefix は配信を許す接頭辞。**バケット全体を許さない。**
//
// Cookie の Path もここに合わせる（画像の取得にだけ送るため）。
const CDNAllowedPrefix = "variants/"

// NewCDNSigner を作る。
//
// privateKeyPEM は CloudFront の公開鍵と対になる秘密鍵である。
// PKCS#1（"RSA PRIVATE KEY"）と PKCS#8（"PRIVATE KEY"）の両方を受け付ける。
func NewCDNSigner(domain, keyPairID, privateKeyPEM string) (*CDNSigner, error) {
	if domain == "" || keyPairID == "" || privateKeyPEM == "" {
		return nil, errors.New("配信ドメイン・鍵ID・秘密鍵のすべてが必要である")
	}

	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}

	return &CDNSigner{
		domain:    domain,
		keyPairID: keyPairID,
		key:       key,
		resource:  "https://" + domain + "/" + CDNAllowedPrefix + "*",
	}, nil
}

// DisplayURL は表示用の URL を返す。
//
// **ttl を使わない。** 有効期限は Cookie が持っており、URL 側には入らない。
// これがキャッシュを効かせるための条件である。
func (c *CDNSigner) DisplayURL(_ context.Context, key string, _ time.Duration) (string, error) {
	if !strings.HasPrefix(key, CDNAllowedPrefix) {
		// **配信できない鍵を黙って返さない。** originals/ を渡された場合、
		// URL としては組み立てられてしまうが、EXIF の残った画像を
		// 配ることになる。ここで止める。
		return "", fmt.Errorf("配信できない鍵である（%s 配下のみ）: %s", CDNAllowedPrefix, key)
	}
	return "https://" + c.domain + "/" + key, nil
}

// SignedCookie は CloudFront が要求する Cookie 1つ分。
type SignedCookie struct {
	Name  string
	Value string
}

/*
SignedCookies は variants/ 配下を読むための Cookie 3つを返す。

**URL ではなく Cookie に署名を載せる理由は、枚数である。**
フィード1ページで最大80枚の画像を返す。URL ごとに署名すると、
応答の中身が署名文字列で埋まる（1本あたり500文字前後になる）。
Cookie なら1セットで全部に効く。

形式は CloudFront が定めている。

  - ポリシー（JSON）を base64 して CloudFront-Policy
  - **同じ JSON を RSA-SHA1 で署名**して base64 し CloudFront-Signature
  - 鍵の ID を CloudFront-Key-Pair-Id

base64 は素のままでは使えず、Cookie で使えない文字を置き換える
（+ → -、= → _、/ → ~）。
*/
func (c *CDNSigner) SignedCookies(expiresAt time.Time) ([]SignedCookie, error) {
	policy := cdnPolicy{
		Statement: []cdnStatement{{
			Resource: c.resource,
			Condition: cdnCondition{
				DateLessThan: cdnEpoch{EpochTime: expiresAt.Unix()},
			},
		}},
	}

	// **符号化したものと署名したものが同じバイト列でなければならない。**
	// 別々に組み立てると、空白の有無だけで検証が落ちる。
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("ポリシーを組み立てられない: %w", err)
	}

	// CloudFront は RSA-SHA1 を要求する。強度の選択ではなく仕様である。
	digest := sha1.Sum(raw) //nolint:gosec // 同上
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA1, digest[:])
	if err != nil {
		return nil, fmt.Errorf("ポリシーに署名できない: %w", err)
	}

	return []SignedCookie{
		{Name: "CloudFront-Policy", Value: cdnBase64(raw)},
		{Name: "CloudFront-Signature", Value: cdnBase64(signature)},
		{Name: "CloudFront-Key-Pair-Id", Value: c.keyPairID},
	}, nil
}

// CDNCookieNames は消すときに使う。**発行と削除で名前がずれると消えない。**
func CDNCookieNames() []string {
	return []string{"CloudFront-Policy", "CloudFront-Signature", "CloudFront-Key-Pair-Id"}
}

// ---------------------------------------------------------------------------

type cdnPolicy struct {
	Statement []cdnStatement `json:"Statement"`
}

type cdnStatement struct {
	Resource  string       `json:"Resource"`
	Condition cdnCondition `json:"Condition"`
}

type cdnCondition struct {
	DateLessThan cdnEpoch `json:"DateLessThan"`
}

type cdnEpoch struct {
	EpochTime int64 `json:"AWS:EpochTime"`
}

// cdnBase64 は CloudFront が定める置き換えを行った base64 を返す。
//
// **素の base64 は Cookie の値に使えない文字を含む。**
func cdnBase64(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	return strings.NewReplacer("+", "-", "=", "_", "/", "~").Replace(s)
}

func parseRSAPrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("秘密鍵を PEM として読めない")
	}

	// PKCS#1（"RSA PRIVATE KEY"）
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// PKCS#8（"PRIVATE KEY"）
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("秘密鍵を解釈できない: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		// CloudFront の公開鍵は RSA しか受け付けない。
		return nil, errors.New("秘密鍵が RSA ではない")
	}
	return key, nil
}

var _ URLSigner = (*CDNSigner)(nil)
