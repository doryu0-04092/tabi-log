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
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui.css">
</head>
<body>
<div id="ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui-bundle.js"></script>
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
func (h *docsHandler) GetDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(docsPage)
}

type docsHandler struct{}
