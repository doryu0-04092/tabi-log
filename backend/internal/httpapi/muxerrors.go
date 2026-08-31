package httpapi

import (
	"net/http"
	"strings"
)

/*
ServeMux が自分で返す 404 / 405 を JSON に揃える。

パスが存在しない（404）ときも、パスはあるがメソッドが違う（405）ときも、
応答を書くのは ServeMux 自身であってハンドラではない。**そこを通らないため、
既定のままだと `text/plain` が返る。** クライアントは応答を JSON として
読むので、エラーの中身を読もうとして別のエラーになる。

以前は `mux.HandleFunc("/", ...)` で受け皿を置いていたが、**それだと
405 が一度も発生しない。** 受け皿が全パス・全メソッドに一致するため、
メソッド違いも 404 に吸収されていた。どのメソッドなら通るのかを
クライアントが知る手段が無くなる。

受け皿をやめ、ServeMux の書き出しを横取りする形にした。
405 の `Allow` ヘッダーは ServeMux が付けたものをそのまま残す。

`docs/audit-2026-08-31.md` L3。
*/

// WithJSONMuxErrors は ServeMux が返す 404 / 405 の本文を JSON に差し替える。
func WithJSONMuxErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&muxErrorWriter{ResponseWriter: w, req: r}, r)
	})
}

type muxErrorWriter struct {
	http.ResponseWriter
	req *http.Request

	// replaced が true の間、本体が書く本文を捨てる。
	// **捨てないと JSON のあとに text/plain が続く。**
	replaced bool
}

// muxErrors は差し替える状態と、返すエラーの内容。
var muxErrors = map[int]struct{ code, message string }{
	http.StatusNotFound:         {"not_found", "エンドポイントが存在しません"},
	http.StatusMethodNotAllowed: {"method_not_allowed", "このパスではそのメソッドを使えません"},
}

func (w *muxErrorWriter) WriteHeader(status int) {
	e, target := muxErrors[status]
	if !target {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	// **ハンドラが返した 404 には触らない。** 「投稿が見つからない」等は
	// 既に JSON で理由まで返しており、それを潰すと理由が失われる。
	if strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	w.replaced = true
	writeError(w.ResponseWriter, w.req, status, e.code, e.message)
}

func (w *muxErrorWriter) Write(b []byte) (int, error) {
	if w.replaced {
		// 書いたことにして捨てる。**エラーを返すと本体が「書き込みに
		// 失敗した」と記録し、実際には失敗していないものが記録に残る。**
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
