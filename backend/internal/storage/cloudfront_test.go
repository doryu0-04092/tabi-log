package storage

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // CloudFront の署名は RSA-SHA1 と決まっている
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

/*
CloudFront の署名付き Cookie。

**この経路はローカルでも E2E でも通らない**（LocalStack に CloudFront が無い）。
本番でしか動かないものを実装したまま検証しない状態にしないため、
形式が仕様どおりであることをここで固定する。

見るのは4つ。

  - URL が固定であること（キャッシュが効くための条件）
  - variants/ 以外を配らないこと（originals/ には EXIF が残っている）
  - 署名がポリシーと対応していること
  - Cookie で使えない文字が残っていないこと
*/

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("鍵を作れない: %v", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, string(encoded)
}

func Test表示用URLは毎回同じになる(t *testing.T) {
	_, pemText := testKey(t)
	signer, err := NewCDNSigner("d123.cloudfront.net", "K123", pemText)
	if err != nil {
		t.Fatalf("作れない: %v", err)
	}

	const key = "variants/abc_thumb.jpg"
	first, err := signer.DisplayURL(t.Context(), key, time.Hour)
	if err != nil {
		t.Fatalf("URL を作れない: %v", err)
	}
	// **時刻を挟んでも変わらないこと。** ここが変わると、
	// エッジにもブラウザにもキャッシュが載らない（署名付き URL の問題そのもの）。
	second, err := signer.DisplayURL(t.Context(), key, 2*time.Hour)
	if err != nil {
		t.Fatalf("URL を作れない: %v", err)
	}

	if first != second {
		t.Errorf("URL が変わった:\n1回目 %s\n2回目 %s", first, second)
	}
	if want := "https://d123.cloudfront.net/variants/abc_thumb.jpg"; first != want {
		t.Errorf("URL が %q。%q のはず", first, want)
	}
	// 署名が URL に混ざっていないこと。
	if strings.Contains(first, "?") {
		t.Errorf("URL にクエリが付いている: %s", first)
	}
}

/*
**originals/ を配らないこと。**

アップロードされたままの画像には EXIF が残っており、GPS 座標を含む。
EXIF を落とすのは変換の工程であり、その成果物が variants/ である。
ここを取り違えると、位置情報を県単位に絞るという判断が画像経由で無効になる。
*/
func Test変換前の画像は配信できない(t *testing.T) {
	_, pemText := testKey(t)
	signer, _ := NewCDNSigner("d123.cloudfront.net", "K123", pemText)

	for _, key := range []string{
		"originals/abc.jpg",
		"abc.jpg",
		"../variants/abc.jpg",
		"",
	} {
		if url, err := signer.DisplayURL(t.Context(), key, time.Hour); err == nil {
			t.Errorf("%q を配信できてしまった: %s", key, url)
		}
	}
}

func Test署名付きCookieが仕様どおりの形になる(t *testing.T) {
	key, pemText := testKey(t)
	signer, err := NewCDNSigner("d123.cloudfront.net", "K123", pemText)
	if err != nil {
		t.Fatalf("作れない: %v", err)
	}

	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	cookies, err := signer.SignedCookies(expires)
	if err != nil {
		t.Fatalf("Cookie を作れない: %v", err)
	}

	got := map[string]string{}
	for _, c := range cookies {
		got[c.Name] = c.Value
	}

	for _, name := range CDNCookieNames() {
		if got[name] == "" {
			t.Fatalf("%s が空である", name)
		}
	}
	if got["CloudFront-Key-Pair-Id"] != "K123" {
		t.Errorf("鍵の ID が %q", got["CloudFront-Key-Pair-Id"])
	}

	// **Cookie の値に使えない文字が残っていないこと。**
	// 素の base64 は + / = を含み、そのままでは Cookie に入れられない。
	for name, value := range got {
		if strings.ContainsAny(value, "+/=") {
			t.Errorf("%s に置き換え漏れがある: %s", name, value)
		}
	}

	// --- ポリシーを復元して中身を確かめる ---
	raw := decodeCDN(t, got["CloudFront-Policy"])

	var policy cdnPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("ポリシーを読めない: %v\n%s", err, raw)
	}
	if len(policy.Statement) != 1 {
		t.Fatalf("Statement が %d 件", len(policy.Statement))
	}
	if want := "https://d123.cloudfront.net/variants/*"; policy.Statement[0].Resource != want {
		t.Errorf("許可範囲が %q。%q のはず", policy.Statement[0].Resource, want)
	}
	if got := policy.Statement[0].Condition.DateLessThan.EpochTime; got != expires.Unix() {
		t.Errorf("期限が %d。%d のはず", got, expires.Unix())
	}

	// --- 署名がポリシーと対応していること ---
	//
	// **符号化したものと署名したものが同じバイト列でなければ、
	// CloudFront 側で検証が落ちる。** 空白の有無だけで壊れる。
	signature := decodeCDN(t, got["CloudFront-Signature"])
	digest := sha1.Sum(raw) //nolint:gosec // 同上
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, digest[:], signature); err != nil {
		t.Errorf("署名がポリシーと対応していない: %v", err)
	}
}

func Test秘密鍵はPKCS8でも読める(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("鍵を作れない: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("PKCS#8 にできない: %v", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	if _, err := NewCDNSigner("d123.cloudfront.net", "K123", string(encoded)); err != nil {
		t.Errorf("PKCS#8 の鍵を読めない: %v", err)
	}
}

// 設定が中途半端なまま動かさない。
func Test設定が欠けていれば作れない(t *testing.T) {
	_, pemText := testKey(t)

	cases := map[string][3]string{
		"ドメインが無い":   {"", "K123", pemText},
		"鍵IDが無い":    {"d123.cloudfront.net", "", pemText},
		"秘密鍵が無い":    {"d123.cloudfront.net", "K123", ""},
		"秘密鍵が壊れている": {"d123.cloudfront.net", "K123", "not a pem"},
	}

	for name, args := range cases {
		if _, err := NewCDNSigner(args[0], args[1], args[2]); err == nil {
			t.Errorf("%s のに作れてしまった", name)
		}
	}
}

// decodeCDN は CloudFront 形式の base64 を戻す。
func decodeCDN(t *testing.T, s string) []byte {
	t.Helper()
	restored := strings.NewReplacer("-", "+", "_", "=", "~", "/").Replace(s)
	raw, err := base64.StdEncoding.DecodeString(restored)
	if err != nil {
		t.Fatalf("base64 を戻せない: %v", err)
	}
	return raw
}

/*
AWS が公開している条件に対する検証。

**この経路は apply するまで実物で確かめられない**ため、
仕様側から取れる条件はすべてここで固定する。
出所は CloudFront 開発者ガイドの「署名付き Cookie（カスタムポリシー）」と
「Linux コマンドと OpenSSL」（2026-08-29 に参照）。
*/

// AWS のドキュメントに載っている例。**公式が示した入力と出力の組である。**
// 置換（+=/ → -_~）まで含めて、こちらの実装が同じ答えを出すかを見る。
const (
	awsExamplePolicy = `{"Statement":[{"Resource":"http://d111111abcdef8.cloudfront.net/game_download.zip",` +
		`"Condition":{"IpAddress":{"AWS:SourceIp":"192.0.2.0/24"},` +
		`"DateLessThan":{"AWS:EpochTime":1426500000}}}]}`

	awsExampleEncoded = "eyJTdGF0ZW1lbnQiOlt7IlJlc291cmNlIjoiaHR0cDovL2QxMTExMTFhYmNkZWY4LmNsb3VkZnJvbnQubmV0" +
		"L2dhbWVfZG93bmxvYWQuemlwIiwiQ29uZGl0aW9uIjp7IklwQWRkcmVzcyI6eyJBV1M6U291cmNlSXAiOiIxOTIu" +
		"MC4yLjAvMjQifSwiRGF0ZUxlc3NUaGFuIjp7IkFXUzpFcG9jaFRpbWUiOjE0MjY1MDAwMDB9fX1dfQ__"
)

func Test符号化がAWSの例と一致する(t *testing.T) {
	if got := cdnBase64([]byte(awsExamplePolicy)); got != awsExampleEncoded {
		t.Errorf("AWS の例と一致しない\n実装: %s\n公式: %s", got, awsExampleEncoded)
	}
}

/*
ポリシーに空白を入れないこと。

AWS は「ポリシーからすべての空白（タブと改行を含む）を取り除く」と定めている。
**署名するのも符号化するのも、この空白を除いた同じバイト列でなければならない。**
json.MarshalIndent に変えると黙って壊れるため、ここで固定する。
*/
func Testポリシーに空白が入らない(t *testing.T) {
	_, pemText := testKey(t)
	signer, _ := NewCDNSigner("d123.cloudfront.net", "K123", pemText)

	cookies, err := signer.SignedCookies(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Cookie を作れない: %v", err)
	}
	raw := string(decodeCDN(t, cookieValue(cookies, "CloudFront-Policy")))

	if strings.ContainsAny(raw, " \t\r\n") {
		t.Errorf("ポリシーに空白が含まれている: %q", raw)
	}
}

/*
有効期限を引用符で囲まないこと。

AWS は「値を引用符で囲まないこと」と明記している。
**文字列にすると CloudFront が読めない。**
*/
func Test有効期限が数値として書かれる(t *testing.T) {
	_, pemText := testKey(t)
	signer, _ := NewCDNSigner("d123.cloudfront.net", "K123", pemText)

	expires := time.Unix(1426500000, 0)
	cookies, err := signer.SignedCookies(expires)
	if err != nil {
		t.Fatalf("Cookie を作れない: %v", err)
	}
	raw := string(decodeCDN(t, cookieValue(cookies, "CloudFront-Policy")))

	if !strings.Contains(raw, `"AWS:EpochTime":1426500000`) {
		t.Errorf("有効期限が数値になっていない: %s", raw)
	}
	if strings.Contains(raw, `"AWS:EpochTime":"`) {
		t.Errorf("有効期限が文字列になっている: %s", raw)
	}
}

/*
Statement は1つだけ、Resource は http:// か https:// で始まること。

AWS の定め:「1つの Statement しか含められない」
「値は http:// または https:// で始まらなければならない」。
*/
func Testポリシーの形が仕様の制約を満たす(t *testing.T) {
	_, pemText := testKey(t)
	signer, _ := NewCDNSigner("d123.cloudfront.net", "K123", pemText)

	cookies, err := signer.SignedCookies(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Cookie を作れない: %v", err)
	}

	var policy cdnPolicy
	if err := json.Unmarshal(decodeCDN(t, cookieValue(cookies, "CloudFront-Policy")), &policy); err != nil {
		t.Fatalf("ポリシーを読めない: %v", err)
	}

	if len(policy.Statement) != 1 {
		t.Errorf("Statement が %d 件。1件しか含められない", len(policy.Statement))
	}
	resource := policy.Statement[0].Resource
	if !strings.HasPrefix(resource, "https://") && !strings.HasPrefix(resource, "http://") {
		t.Errorf("Resource が %q。http:// か https:// で始まる必要がある", resource)
	}
}

/*
Cookie の名前は大文字小文字を区別する。

AWS は「Cookie の属性名は大文字小文字を区別する」と明記している。
**綴りが1文字違うだけで、CloudFront は署名が無いものとして扱う。**
*/
func TestCookieの名前が仕様どおりである(t *testing.T) {
	want := []string{"CloudFront-Policy", "CloudFront-Signature", "CloudFront-Key-Pair-Id"}

	got := CDNCookieNames()
	if len(got) != len(want) {
		t.Fatalf("Cookie が %d 個。3個のはず", len(got))
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("%d番目が %q。%q のはず", i, got[i], name)
		}
	}

	// **CloudFront-Hash-Algorithm は出さない。**
	// SHA-1 を使う場合は不要で、SHA-256 に変えたときだけ必要になる。
	for _, name := range got {
		if name == "CloudFront-Hash-Algorithm" {
			t.Error("SHA-1 なのに CloudFront-Hash-Algorithm を出している")
		}
	}
}

func cookieValue(cookies []SignedCookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
