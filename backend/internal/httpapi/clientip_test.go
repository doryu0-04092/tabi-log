package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// レート制限の鍵に使う発信元の決め方。
//
// **ここを誤ると、レート制限は「無い」か「全員を巻き込む」のどちらかになる。**
// 左に寄せすぎるとクライアントが名乗った値を信じて素通りし、
// 右に寄せすぎると全員がプロキシのIPとして1つの鍵に潰れる。
// どちらも表向きは動いて見えるため、テストで固定する。
func TestClientIP(t *testing.T) {
	newRequest := func(remoteAddr, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.RemoteAddr = remoteAddr
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	tests := []struct {
		name       string
		trust      bool
		hops       int
		remoteAddr string
		xff        string
		want       string
	}{
		{
			name:       "ヘッダーを信用しない場合は接続元を使う",
			trust:      false,
			hops:       1,
			remoteAddr: "192.0.2.10:54321",
			xff:        "1.2.3.4, 203.0.113.9, 130.176.0.1",
			want:       "192.0.2.10",
		},
		{
			// 本番の形。CloudFront → ALB → アプリ の2段。
			name:       "二段構成では末尾から1つ手前が利用者になる",
			trust:      true,
			hops:       1,
			remoteAddr: "10.0.1.5:443",
			xff:        "1.2.3.4, 203.0.113.9, 130.176.0.1",
			want:       "203.0.113.9",
		},
		{
			name:       "ALB直結の想定では末尾が利用者になる",
			trust:      true,
			hops:       0,
			remoteAddr: "10.0.1.5:443",
			xff:        "1.2.3.4, 203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			// **左へ寄せない。** 末尾はどの構成でも直前のプロキシが自分で書いた値であり、
			// クライアントには操作できない。届かないなら安全側へ倒す。
			name:       "段数が想定より少なければ末尾へ倒す",
			trust:      true,
			hops:       3,
			remoteAddr: "10.0.1.5:443",
			xff:        "1.2.3.4, 203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			name:       "ヘッダーが無ければ接続元を使う",
			trust:      true,
			hops:       1,
			remoteAddr: "192.0.2.10:54321",
			want:       "192.0.2.10",
		},
		{
			name:       "空白があっても取り違えない",
			trust:      true,
			hops:       1,
			remoteAddr: "10.0.1.5:443",
			xff:        "  1.2.3.4 ,  203.0.113.9  , 130.176.0.1 ",
			want:       "203.0.113.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &authHandler{opts: AuthOptions{
				TrustProxyHeaders: tt.trust,
				TrustedProxyHops:  tt.hops,
			}}
			if got := h.clientIP(newRequest(tt.remoteAddr, tt.xff)); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

// **最も重要な1件。**
//
// 攻撃者がリクエストごとに偽のIPを名乗っても、鍵が変わらないこと。
// 変わってしまうと、レート制限は事実上存在しないのと同じになる。
func TestClientIPKeepsKeyUnderSpoofedHeader(t *testing.T) {
	h := &authHandler{opts: AuthOptions{TrustProxyHeaders: true, TrustedProxyHops: 1}}

	newRequest := func(xff string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.RemoteAddr = "10.0.1.5:443"
		r.Header.Set("X-Forwarded-For", xff)
		return r
	}

	// 左端だけを入れ替える。ここはクライアントが自由に書ける領域である。
	first := h.clientIP(newRequest("9.9.9.9, 203.0.113.9, 130.176.0.1"))
	second := h.clientIP(newRequest("8.8.8.8, 203.0.113.9, 130.176.0.1"))

	if first != second {
		t.Fatalf("詐称で鍵が変わった: %q と %q", first, second)
	}
	if first != "203.0.113.9" {
		t.Fatalf("利用者のIPを取れていない: %q", first)
	}
}
