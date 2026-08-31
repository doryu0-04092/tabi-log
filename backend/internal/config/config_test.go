package config

import (
	"strings"
	"testing"
)

/*
本番で成立しない設定を起動時に弾く。

**どれも「動いてしまう」種類の間違いである。** 起動は成功し、画面も動く。
壊れているのは守りの部分だけなので、動かして気づく機会が無い。
**起動を止めるのが唯一の検知手段になる。**

`docs/audit-2026-08-31.md` L1。
*/

// 本番として成立している設定。各テストはここから1つだけ崩す。
func productionConfig() Config {
	c := Config{Env: envProduction}
	c.Auth.CookieSecure = true
	c.Auth.TrustProxyHeaders = true
	c.Storage.Endpoint = ""
	c.Storage.CDN.Domain = "d123.cloudfront.net"
	return c
}

func Test本番の設定が揃っていれば通る(t *testing.T) {
	if err := checkProduction(productionConfig(), RoleServer); err != nil {
		t.Fatalf("揃っているのに弾かれた: %v", err)
	}
}

func Test本番で成立しない設定は起動を止める(t *testing.T) {
	tests := map[string]struct {
		breakIt func(*Config)
		want    string
	}{
		"Cookie が平文の経路にも送られる": {
			func(c *Config) { c.Auth.CookieSecure = false },
			"COOKIE_SECURE",
		},
		"発信元が全員 ALB になり制限が潰れる": {
			func(c *Config) { c.Auth.TrustProxyHeaders = false },
			"TRUST_PROXY_HEADERS",
		},
		"ローカル向けの S3 指定が残っている": {
			func(c *Config) { c.Storage.Endpoint = "http://localstack:4566" },
			"STORAGE_S3_ENDPOINT",
		},
		"画像が CloudFront を通らない": {
			func(c *Config) { c.Storage.CDN.Domain = "" },
			"CDN_DOMAIN",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := productionConfig()
			tt.breakIt(&c)
			err := checkProduction(c, RoleServer)
			if err == nil {
				t.Fatalf("起動が止まらなかった")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("どの設定が問題かが分からない。%q を含むべき: %v", tt.want, err)
			}
		})
	}
}

// ローカルでは同じ設定でも起動できなければならない。
// **本番の条件をローカルに持ち込むと開発ができなくなる。**
func Testローカルでは何も弾かない(t *testing.T) {
	c := Config{Env: "local"}
	if err := checkProduction(c, RoleServer); err != nil {
		t.Fatalf("ローカルで弾かれた: %v", err)
	}
}

// 画像処理は Cookie も CDN も扱わない。**同じ条件を課すと起動できない。**
// JWT も発行しないため、使わない秘密を渡す必要も無い。
func Test画像処理には本番の条件を課さない(t *testing.T) {
	c := Config{Env: envProduction}
	if err := checkProduction(c, RoleImageWorker); err != nil {
		t.Fatalf("画像処理が弾かれた: %v", err)
	}
}
