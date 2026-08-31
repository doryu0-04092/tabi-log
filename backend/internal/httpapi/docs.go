package httpapi

import (
	_ "embed"
	"net/http"
)

/*
API 仕様をブラウザから読めるようにする。

**仕様書は docs/openapi.yaml が唯一の正である。** ここで配るのは
その写しであり、`go generate` が置き直す。写しが古くなる余地を
残さないため、**CI の「生成物の一致検証」が差分を落とす。**
手で編集すると CI が落ちる。

---

**Swagger UI の実体は CDN から読む。**

同梱すると数 MB のファイル群をリポジトリに抱えることになり、
更新のたびに差分が出る。版は固定してあり、
**その版のまま置き換わらない**（jsdelivr は同じ URL の内容を変えない）。

**ただし「置き換わらない」は CDN が正常である限りの話である。**
このページは同一オリジンで開くため、CDN 側が改ざんされると、
そのスクリプトから `/api/auth/refresh` を呼べる。CSRF 用の
ヘッダーは固定文字列なので防げず、閲覧中の利用者の
アクセストークンが取られうる。

対処は2つ重ねてある。

  - **`integrity` を付ける。** 中身が変わればブラウザが読み込みを拒否する
  - **本番では配らない。** 危険が残るのは「本物の利用者が本番で開いたとき」
    であり、その状況を作らない

CloudFront の CSP も `script-src 'self'` で外部スクリプトを塞いでいる
（infra/cloudfront.tf）。**本番では三重に届かない。**

代償として、**この画面はネットワークが繋がらない環境では表示できない。**
仕様そのものは /api/openapi.yaml から素のまま取れるので、
読む手段が完全に失われるわけではない。

---

**公開範囲についての判断。**

この API は公開 API ではないため、仕様を誰でも読める場所に置くことは
「攻撃の下調べを楽にする」側面がある。それでも配っているのは、
**隠しても守りにならない**（エンドポイントは画面の通信を見れば分かる）
一方で、仕様が読めることの利点（画面側との突き合わせ、課題としての提出）
が大きいためである。**認可の担保はサーバー側で行っており、
仕様を隠すことに依存していない。**
*/

// docs/openapi.yaml の写し。`go generate` が置く。手で編集しない。
//
//go:embed openapi.yaml
var openAPISpec []byte

// swaggerUIVersion は読み込む Swagger UI の版。**必ず固定する。**
// latest を指すと、こちらが何もしていないのに画面が変わる。
const swaggerUIVersion = "5.29.4"

// 完全性の検証に使うハッシュ。**版を上げたら必ず取り直す。**
// 取り違えると画面が真っ白になるだけで、原因はコンソールにしか出ない。
//
//	curl -sfL https://cdn.jsdelivr.net/npm/swagger-ui-dist@<版>/<ファイル> |
//	  openssl dgst -sha384 -binary | openssl base64 -A
const (
	swaggerUIBundleHash = "sha384-eGAqzBSdqmAnsjFjrz0Ua2nJFnpAzDMmRg4mr6jwRwzcjSmL9FMmXAhMwX+mTFfs"
	swaggerUICSSHash    = "sha384-++DMKo1369T5pxDNqojF1F91bYxYiT1N7b1M15a7oCzEodfljztKlApQoH6eQSKI"
)

// docsPage は Swagger UI を開くだけの HTML。
//
// 仕様は同じオリジンの /api/openapi.yaml から読ませる。
// CDN 側に仕様を渡さないためである。
var docsPage = []byte(`<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tabi-log API 仕様</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui.css"
      integrity="` + swaggerUICSSHash + `" crossorigin="anonymous">
</head>
<body>
<div id="ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui-bundle.js"
        integrity="` + swaggerUIBundleHash + `" crossorigin="anonymous"></script>
<script>
	window.ui = SwaggerUIBundle({
		url: '/api/openapi.yaml',
		dom_id: '#ui',
		// 「試してみる」は出さない。認証を通さずに叩いて 401 を見るだけになり、
		// 動かし方の説明にならない。実際に叩くなら画面か curl を使う。
		supportedSubmitMethods: []
	});
</script>
</body>
</html>
`)

// GetOpenAPISpec は仕様を YAML のまま返す。
func (h *docsHandler) GetOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	// 版が変わればファイルごと変わる。短めに持たせる。
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openAPISpec)
}

// GetDocs は仕様を読むための画面を返す。
// GetDocs は Swagger UI を返す。**本番では 404 を返す。**
//
// このページは外部 CDN のスクリプトを同一オリジンで動かす。
// integrity を付けてあるが、**本物の利用者が本番で開く状況を
// そもそも作らない**のが最も確実である。
func (h *docsHandler) GetDocs(w http.ResponseWriter, r *http.Request) {
	if !h.exposeUI {
		writeError(w, r, http.StatusNotFound, "not_found", "エンドポイントが存在しません")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(docsPage)
}

// docsHandler は仕様と、それを読むための画面を配る。
type docsHandler struct {
	// exposeUI が false なら画面を配らない。**本番で false にする。**
	//
	// 仕様そのもの（/api/openapi.yaml）は配り続ける。危険なのは
	// 外部スクリプトを同一オリジンで動かすことであって、
	// 仕様が読めること自体ではない。
	exposeUI bool
}
