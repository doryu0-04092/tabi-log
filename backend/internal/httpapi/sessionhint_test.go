package httpapi

import (
	"net/http"
	"testing"
)

/*
セッションの有無を示す印。

**画面が「復元を試みるべきか」を判断するためだけのものである。**
これが無いと、未ログインの人が開くたびに /api/auth/refresh へ問い合わせ、
必ず 401 が返る。無駄な往復で最初の描画が遅れ、
ブラウザのコンソールにもエラーが残る。

**秘密を含まないことが前提の設計である。** その前提が崩れていないかを
ここで固定する。
*/

func Test登録すると印のCookieが置かれる(t *testing.T) {
	h := newRouter(t, testDeps{})

	rec := doJSON(h, req(http.MethodPost, "/api/auth/signup",
		`{"email":"a@example.test","handle":"tabi","displayName":"旅人","password":"password12345"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("登録が %d で返った: %s", rec.Code, rec.Body.String())
	}

	hint := cookieByName(rec, sessionHintCookieName)
	if hint == nil {
		t.Fatal("印の Cookie が置かれていない")
	}

	// **値に意味を持たせない。** 利用者 ID や期限を入れると、
	// 「読めても害が無い」という前提が崩れる。
	if hint.Value != "1" {
		t.Errorf("印の値が %q。\"1\" のはず", hint.Value)
	}
	// **HttpOnly にしない。** 画面が読めることが目的である。
	if hint.HttpOnly {
		t.Error("印が HttpOnly になっている。画面から読めないと意味が無い")
	}
	// どの画面からでも読める必要がある。
	if hint.Path != "/" {
		t.Errorf("印の Path が %q。\"/\" のはず", hint.Path)
	}

	// **寿命はリフレッシュトークンと揃える。** ずれると、
	// 印はあるのにトークンが無い（無駄な 401）か、その逆になる。
	refresh := cookieByName(rec, refreshCookieName)
	if refresh == nil {
		t.Fatal("リフレッシュトークンの Cookie が置かれていない")
	}
	if hint.MaxAge != refresh.MaxAge {
		t.Errorf("印の MaxAge が %d、トークンが %d。揃っているはず", hint.MaxAge, refresh.MaxAge)
	}

	// **トークンそのものが印に混ざっていないこと。**
	if hint.Value == refresh.Value {
		t.Error("印にリフレッシュトークンが入っている")
	}
}

func Testログアウトすると印も消える(t *testing.T) {
	h := newRouter(t, testDeps{})

	r := req(http.MethodPost, "/api/auth/logout", "")
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "some-token"})
	r.Header.Set("X-Requested-With", "tabi-log")
	rec := doJSON(h, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("ログアウトが %d で返った: %s", rec.Code, rec.Body.String())
	}

	// **印だけが残ると、画面は毎回 401 を踏みに行くことになる。**
	hint := cookieByName(rec, sessionHintCookieName)
	if hint == nil {
		t.Fatal("印を消す Cookie が返っていない")
	}
	if hint.MaxAge >= 0 {
		t.Errorf("印の MaxAge が %d。負の値で削除を指示するはず", hint.MaxAge)
	}
}

// リフレッシュが失敗したときも印を消す。
// **消さないと、無効なトークンを持ったまま毎回問い合わせ続ける。**
func Testリフレッシュに失敗すると印も消える(t *testing.T) {
	h := newRouter(t, testDeps{})

	r := req(http.MethodPost, "/api/auth/refresh", "")
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "not-a-real-token"})
	r.Header.Set("X-Requested-With", "tabi-log")
	rec := doJSON(h, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("リフレッシュが %d で返った。401 を期待した", rec.Code)
	}
	hint := cookieByName(rec, sessionHintCookieName)
	if hint == nil || hint.MaxAge >= 0 {
		t.Error("失敗したのに印が消えていない")
	}
}
