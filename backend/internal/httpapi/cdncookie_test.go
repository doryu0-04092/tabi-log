package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
)

/*
画像配信の署名付き Cookie が、認証の経路で置かれ・消えること。

**署名の中身は internal/storage で見ている。** ここで見るのは配線だけ、
つまり「ログインで置かれるか」「ログアウトで消えるか」「設定が無いときに
余計なものを置かないか」である。
*/

func cdnTestSigner(t *testing.T) *storage.CDNSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("鍵を作れない: %v", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	signer, err := storage.NewCDNSigner("d123.cloudfront.net", "K123", string(encoded))
	if err != nil {
		t.Fatalf("署名係を作れない: %v", err)
	}
	return signer
}

const cdnSignupBody = `{"email":"cdn@example.test","handle":"cdnuser","displayName":"配信","password":"password12345"}`

func Test登録すると画像配信のCookieが置かれる(t *testing.T) {
	h := newRouter(t, testDeps{cdn: cdnTestSigner(t)})

	rec := doJSON(h, req(http.MethodPost, "/api/auth/signup", cdnSignupBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("登録が %d で返った: %s", rec.Code, rec.Body.String())
	}

	for _, name := range storage.CDNCookieNames() {
		c := cookieByName(rec, name)
		if c == nil {
			t.Fatalf("%s が置かれていない", name)
		}
		if c.Value == "" {
			t.Errorf("%s が空である", name)
		}
		// **画像の取得にだけ送る。** API の全リクエストに3つ載せる必要は無い。
		if c.Path != "/variants/" {
			t.Errorf("%s の Path が %q。\"/variants/\" のはず", name, c.Path)
		}
		// 画面から読む必要は無い。
		if !c.HttpOnly {
			t.Errorf("%s が HttpOnly でない", name)
		}
		// **Strict にしない。** 外部リンクから開いた最初の描画で
		// 画像が出ないことになる。
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s の SameSite が %v。Lax のはず", name, c.SameSite)
		}

		// **Expires と Max-Age を付けない。**
		//
		// AWS は「除外することを勧める。ブラウザを閉じたときに Cookie が
		// 消え、第三者に使われる余地が減る」としている。
		// 消えても、次に開いたときの復元（/auth/refresh）で置き直される。
		if !c.Expires.IsZero() {
			t.Errorf("%s に Expires が付いている: %v", name, c.Expires)
		}
		if c.MaxAge != 0 {
			t.Errorf("%s に Max-Age が付いている: %d", name, c.MaxAge)
		}

		// **Domain を指定しない。**
		// AWS の定めでは、指定する場合は URL のドメインと一致していなければ
		// ならない。指定しなければ、その Cookie を置いたドメインが既定になる。
		// 画面も画像も同じ配信ドメインなので、指定しない方が食い違わない。
		if c.Domain != "" {
			t.Errorf("%s に Domain が付いている: %q", name, c.Domain)
		}
	}
}

func Testログアウトすると画像配信のCookieも消える(t *testing.T) {
	h := newRouter(t, testDeps{cdn: cdnTestSigner(t)})

	r := req(http.MethodPost, "/api/auth/logout", "")
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "some-token"})
	r.Header.Set("X-Requested-With", "tabi-log")
	rec := doJSON(h, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("ログアウトが %d で返った", rec.Code)
	}

	for _, name := range storage.CDNCookieNames() {
		c := cookieByName(rec, name)
		if c == nil {
			t.Fatalf("%s を消す指示が返っていない", name)
		}
		if c.MaxAge >= 0 {
			t.Errorf("%s の MaxAge が %d。負の値で削除を指示するはず", name, c.MaxAge)
		}
	}
}

// **設定が無ければ何も置かない。**
// ローカルと LocalStack には CloudFront が無く、
// そこで意味の無い Cookie を置くと動作の違いが分かりにくくなる。
func TestCDNの設定が無ければCookieを置かない(t *testing.T) {
	h := newRouter(t, testDeps{})

	rec := doJSON(h, req(http.MethodPost, "/api/auth/signup", cdnSignupBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("登録が %d で返った: %s", rec.Code, rec.Body.String())
	}

	for _, name := range storage.CDNCookieNames() {
		if c := cookieByName(rec, name); c != nil {
			t.Errorf("設定が無いのに %s が置かれている", name)
		}
	}
}
