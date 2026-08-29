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
