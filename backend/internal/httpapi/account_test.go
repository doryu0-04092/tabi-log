package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

const testPassword = "password12345"

func testPasswordHash(t *testing.T) string {
	t.Helper()
	h, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("ハッシュ化に失敗した: %v", err)
	}
	return h
}

func currentUser() domain.User {
	bio := "もとの自己紹介"
	return domain.User{ID: 7, Handle: "traveler", DisplayName: "もとの名前", Bio: &bio}
}

// ---------------------------------------------------------------------------
// プロフィールの編集
// ---------------------------------------------------------------------------

// **送られた項目だけを変える。** 省略した項目まで空になると、
// 表示名だけ直したつもりで自己紹介が消える。
func TestUpdateProfileOnlyChangesGivenFields(t *testing.T) {
	repo := &stubAccountRepo{current: currentUser()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodPatch, "/api/users/me", `{"displayName":"あたらしい名前"}`),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	if len(repo.updated) != 1 {
		t.Fatalf("更新が %d 回。1回を期待した", len(repo.updated))
	}
	got := repo.updated[0]
	if got.DisplayName != "あたらしい名前" {
		t.Fatalf("表示名が %q。更新されていない", got.DisplayName)
	}
	// 自己紹介は送っていないので、もとの値のまま。
	if got.Bio == nil || *got.Bio != "もとの自己紹介" {
		t.Fatalf("送っていない自己紹介が変わっている: %v", got.Bio)
	}
}

// **空文字は「消す」。** 省略との違いに意味がある。
func TestUpdateProfileEmptyBioClearsIt(t *testing.T) {
	repo := &stubAccountRepo{current: currentUser()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})

	doJSON(h, withBearer(
		req(http.MethodPatch, "/api/users/me", `{"bio":""}`),
		mustIssue(t, tokens, 7),
	))

	if len(repo.updated) != 1 || repo.updated[0].Bio != nil {
		t.Fatalf("自己紹介が消えていない: %+v", repo.updated)
	}
	// 表示名は送っていないので、もとの値のまま。
	if repo.updated[0].DisplayName != "もとの名前" {
		t.Fatalf("送っていない表示名が変わっている: %q", repo.updated[0].DisplayName)
	}
}

func TestUpdateProfileRejectsBadLengths(t *testing.T) {
	repo := &stubAccountRepo{current: currentUser()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	cases := map[string]string{
		"表示名が空":     `{"displayName":"   "}`,
		"表示名が長すぎる":  fmt.Sprintf(`{"displayName":%q}`, strings.Repeat("あ", maxDisplayNameRunes+1)),
		"自己紹介が長すぎる": fmt.Sprintf(`{"bio":%q}`, strings.Repeat("あ", maxBioRunes+1)),
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doJSON(h, withBearer(req(http.MethodPatch, "/api/users/me", body), token))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%d が返った。400 を期待した。body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if len(repo.updated) != 0 {
		t.Fatalf("検証に失敗したのに更新されている: %+v", repo.updated)
	}
}

// ---------------------------------------------------------------------------
// パスワードの変更
// ---------------------------------------------------------------------------

func TestChangePasswordRequiresCurrentPassword(t *testing.T) {
	repo := &stubAccountRepo{hash: testPasswordHash(t)}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})

	rec := doJSON(h, withCSRF(withBearer(
		req(http.MethodPut, "/api/users/me/password",
			`{"currentPassword":"wrong-password","newPassword":"newpassword123"}`),
		mustIssue(t, tokens, 7),
	)))
	// **401 にしない。** ログイン状態そのものは有効であり、
	// 401 だと画面がログイン画面へ飛ばしてしまう。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d が返った。400 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.changedHashes) != 0 {
		t.Fatal("現在のパスワードが違うのに変更されている")
	}
}

func TestChangePasswordSucceedsAndClearsCookie(t *testing.T) {
	repo := &stubAccountRepo{hash: testPasswordHash(t)}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})

	rec := doJSON(h, withCSRF(withBearer(
		req(http.MethodPut, "/api/users/me/password",
			fmt.Sprintf(`{"currentPassword":%q,"newPassword":"newpassword123"}`, testPassword)),
		mustIssue(t, tokens, 7),
	)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%d が返った。204 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.changedHashes) != 1 {
		t.Fatalf("変更が %d 回。1回を期待した", len(repo.changedHashes))
	}
	// 保存されるのはハッシュであって平文ではない。
	if strings.Contains(repo.changedHashes[0], "newpassword123") {
		t.Fatal("平文がそのまま渡っている")
	}

	// **全トークンを失効させるため、呼び出した側も入り直しになる。**
	// Cookie を消しておかないと、消えたトークンを送り続けることになる。
	c := cookieByName(rec, refreshCookieName)
	if c == nil || c.MaxAge >= 0 {
		t.Fatalf("Cookie が消されていない: %+v", c)
	}
}

// 登録時と同じ規則で長さを見る。合っていないと、
// 登録できるのに変更できないパスワードが生まれる。
func TestChangePasswordUsesSameLengthRuleAsSignup(t *testing.T) {
	repo := &stubAccountRepo{hash: testPasswordHash(t)}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for _, pw := range []string{"short", strings.Repeat("a", 73)} {
		rec := doJSON(h, withCSRF(withBearer(
			req(http.MethodPut, "/api/users/me/password",
				fmt.Sprintf(`{"currentPassword":%q,"newPassword":%q}`, testPassword, pw)),
			token,
		)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d バイトのパスワードが %d で通った。400 を期待した", len(pw), rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// 退会
// ---------------------------------------------------------------------------

func TestDeleteAccountRequiresCurrentPassword(t *testing.T) {
	repo := &stubAccountRepo{hash: testPasswordHash(t)}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, tokens: tokens})

	rec := doJSON(h, withCSRF(withBearer(
		req(http.MethodDelete, "/api/users/me", `{"currentPassword":"wrong-password"}`),
		mustIssue(t, tokens, 7),
	)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d が返った。400 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	// **取り消せない操作なので、断ったときに何も消えていないことまで確かめる。**
	if len(repo.deletedFor) != 0 {
		t.Fatalf("パスワードが違うのに退会が走っている: %v", repo.deletedFor)
	}
}

func TestDeleteAccountDeletesS3Objects(t *testing.T) {
	repo := &stubAccountRepo{
		hash:       testPasswordHash(t),
		deleteKeys: []string{"originals/a.jpg", "variants/a-thumb.jpg"},
	}
	storage := &stubStorage{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, storage: storage, tokens: tokens})

	rec := doJSON(h, withCSRF(withBearer(
		req(http.MethodDelete, "/api/users/me", fmt.Sprintf(`{"currentPassword":%q}`, testPassword)),
		mustIssue(t, tokens, 7),
	)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%d が返った。204 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.deletedFor) != 1 || repo.deletedFor[0] != 7 {
		t.Fatalf("退会が %v。利用者7を期待した", repo.deletedFor)
	}

	// **S3 は外部キーの連鎖では消えない。** 明示的に消していることを確かめる。
	if len(storage.deleted) != 2 {
		t.Fatalf("削除した鍵が %v。2件を期待した", storage.deleted)
	}

	c := cookieByName(rec, refreshCookieName)
	if c == nil || c.MaxAge >= 0 {
		t.Fatalf("Cookie が消されていない: %+v", c)
	}
}

// **S3 の削除に失敗しても退会は成立させる。**
// 「データベースは消えたが S3 に残る」は棚卸しで拾えるが、逆は表示が壊れる。
func TestDeleteAccountSucceedsWhenStorageFails(t *testing.T) {
	repo := &stubAccountRepo{hash: testPasswordHash(t), deleteKeys: []string{"originals/a.jpg"}}
	storage := &stubStorage{deleteErr: errStorageForTest}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{account: repo, storage: storage, tokens: tokens})

	rec := doJSON(h, withCSRF(withBearer(
		req(http.MethodDelete, "/api/users/me", fmt.Sprintf(`{"currentPassword":%q}`, testPassword)),
		mustIssue(t, tokens, 7),
	)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%d が返った。204 を期待した", rec.Code)
	}
	if len(repo.deletedFor) != 1 {
		t.Fatal("S3 の失敗で退会まで巻き戻っている")
	}
}

func TestAccountEndpointsRequireAuthentication(t *testing.T) {
	repo := &stubAccountRepo{}
	h := newRouter(t, testDeps{account: repo})

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPatch, "/api/users/me", `{"displayName":"x"}`},
		{http.MethodPut, "/api/users/me/password", `{"currentPassword":"a","newPassword":"password123"}`},
		{http.MethodDelete, "/api/users/me", `{"currentPassword":"a"}`},
	}

	for _, c := range cases {
		rec := doJSON(h, withCSRF(req(c.method, c.path, c.body)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s が %d を返した。401 を期待した", c.method, c.path, rec.Code)
		}
	}
	if len(repo.updated) != 0 || len(repo.changedHashes) != 0 || len(repo.deletedFor) != 0 {
		t.Fatal("認証なしのリクエストが repo まで到達している")
	}
}

// ---------------------------------------------------------------------------
// 旅行履歴
// ---------------------------------------------------------------------------

// **カーソルは訪問日と ID の組である。** 訪問日は重複するため、
// 日付だけでは同じ日の中のどこまで返したかを表せない。
func TestListUserTravelsCursorIsDateAndID(t *testing.T) {
	posts := &stubPostRepo{
		posts:            []domain.Post{{ID: 5, Author: domain.User{ID: 9}}},
		nextTravelCursor: store.TravelCursor{VisitedOn: mustDate(t, "2026-05-03"), ID: 5},
	}
	follows := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: posts, follows: follows, tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodGet, "/api/users/traveler/travels?limit=1", ""),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			NextCursor *string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if got.Data.NextCursor == nil || *got.Data.NextCursor != "2026-05-03_5" {
		t.Fatalf("nextCursor が %v。\"2026-05-03_5\" を期待した", got.Data.NextCursor)
	}
}

func TestListUserTravelsParsesCursor(t *testing.T) {
	posts := &stubPostRepo{}
	follows := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: posts, follows: follows, tokens: tokens})

	doJSON(h, withBearer(
		req(http.MethodGet, "/api/users/traveler/travels?cursor=2026-05-03_5", ""),
		mustIssue(t, tokens, 7),
	))

	if posts.lastTravelCursor.ID != 5 {
		t.Fatalf("カーソルの ID が %d。5 を期待した", posts.lastTravelCursor.ID)
	}
	if posts.lastTravelCursor.VisitedOn.Format("2006-01-02") != "2026-05-03" {
		t.Fatalf("カーソルの訪問日が %v", posts.lastTravelCursor.VisitedOn)
	}
}

func TestListUserTravelsRejectsBadCursor(t *testing.T) {
	follows := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: follows, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for _, cursor := range []string{"5", "abc_5", "2026-05-03_abc", "2026-13-99_5"} {
		rec := doJSON(h, withBearer(
			req(http.MethodGet, "/api/users/traveler/travels?cursor="+cursor, ""),
			token,
		))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("cursor=%q が %d で通った。400 を期待した", cursor, rec.Code)
		}
	}
}

func TestListUserTravelsUnknownHandleReturns404(t *testing.T) {
	follows := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: follows, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/nobody/travels", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
}
