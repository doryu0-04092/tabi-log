package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"

	"github.com/golang-jwt/jwt/v5"
	"gopkg.in/yaml.v3"
)

// specPath は仕様ファイルの位置。テストの実行ディレクトリからの相対。
const specPath = "../../../docs/openapi.yaml"

// publicPaths が仕様と食い違うと、認証が要るはずのエンドポイントが
// 素通りするか、公開のはずのものが 401 になる。
//
// **人が2か所を手で揃えるのをやめ、仕様を正として突き合わせる。**
func TestPublicPaths_仕様のsecurity空と一致する(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("仕様ファイルを読めない: %v", err)
	}

	// パス階層には HTTP メソッド以外のキー（parameters など）も並ぶため、
	// いったん生のノードで受けてから、メソッドのものだけを解釈する。
	var spec struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("仕様の解析に失敗した: %v", err)
	}

	httpMethods := map[string]struct{}{
		"get": {}, "post": {}, "put": {}, "patch": {},
		"delete": {}, "head": {}, "options": {}, "trace": {},
	}

	wantPublic := map[string]struct{}{}
	for path, operations := range spec.Paths {
		for method, node := range operations {
			if _, ok := httpMethods[method]; !ok {
				continue
			}
			var op struct {
				Security *[]map[string][]string `yaml:"security"`
			}
			if err := node.Decode(&op); err != nil {
				t.Fatalf("%s %s の解析に失敗した: %v", method, path, err)
			}
			// security: [] が明示されている操作だけが認証不要である。
			// 省略されている場合はトップレベルの security（bearerAuth）が効く。
			if op.Security != nil && len(*op.Security) == 0 {
				wantPublic[apiBasePath+path] = struct{}{}
			}
		}
	}

	if len(wantPublic) == 0 {
		t.Fatal("仕様から認証不要のパスを1つも読み取れなかった（解析が壊れている可能性がある）")
	}

	if missing := diffKeys(wantPublic, publicPaths); len(missing) > 0 {
		t.Errorf("仕様では認証不要だが publicPaths に無い（誤って 401 になる）: %v", missing)
	}
	if extra := diffKeys(publicPaths, wantPublic); len(extra) > 0 {
		t.Errorf("publicPaths にあるが仕様では認証が要る（**認証が素通りする**）: %v", extra)
	}
}

func diffKeys(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestWithAuthentication_トークンが無ければ401を返す(t *testing.T) {
	router := newRouter(t, testDeps{})

	rec := do(t, router, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
	}
	if code := errorCode(t, rec); code != "unauthenticated" {
		t.Errorf("code: 期待 unauthenticated, 実際 %q", code)
	}
}

// 期限切れを区別できないと、クライアントは「リフレッシュすべき」場面で
// 利用者に再ログインを求めてしまう。
func TestWithAuthentication_期限切れは専用のコードを返す(t *testing.T) {
	key := auth.SigningKey{ID: "v1", Method: jwt.SigningMethodHS256, Secret: []byte(testJWTSecret)}
	tokens, err := auth.NewJWTService(key, []auth.SigningKey{key}, time.Minute)
	if err != nil {
		t.Fatalf("作成に失敗した: %v", err)
	}
	expired, _, err := tokens.Issue(1, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("発行に失敗した: %v", err)
	}

	router := newRouter(t, testDeps{tokens: tokens})
	rec := do(t, router, withBearer(httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), expired))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ステータス: 期待 %d, 実際 %d", http.StatusUnauthorized, rec.Code)
	}
	if code := errorCode(t, rec); code != "token_expired" {
		t.Errorf("code: 期待 token_expired, 実際 %q", code)
	}
}

func TestWithAuthentication_公開パスはトークン無しで通る(t *testing.T) {
	router := newRouter(t, testDeps{})

	for path := range publicPaths {
		if path == "/api/auth/signup" || path == "/api/auth/login" ||
			path == "/api/auth/refresh" || path == "/api/auth/logout" {
			// POST 専用のため、ここでは GET 可能なものだけ確認する。
			continue
		}
		rec := do(t, router, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s が 401 になった（公開のはず）", path)
		}
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{"通常", "Bearer abc.def.ghi", "abc.def.ghi", true},
		{"大文字小文字を無視する", "bearer abc", "abc", true},
		{"種別が違う", "Basic abc", "", false},
		{"値が無い", "Bearer ", "", false},
		{"ヘッダーが無い", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			got, ok := bearerToken(r)
			if ok != tt.ok || got != tt.want {
				t.Errorf("期待 (%q, %v), 実際 (%q, %v)", tt.want, tt.ok, got, ok)
			}
		})
	}
}
