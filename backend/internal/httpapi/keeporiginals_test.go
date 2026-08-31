package httpapi

import (
	"errors"
	"net/http"
	"testing"
)

/*
原本の保持印。

投稿が確定した原本は、期限削除の対象から外さなければならない。
**外し忘れると 7 日後に原本が消え、別解像度を後から作れなくなる。**
消えるのが 7 日後であるため、動かして気づくことはまずない。
ここで固定しておく。

`docs/audit-2026-08-31.md` H1。
*/

func Test投稿が確定したら原本に保持印を付ける(t *testing.T) {
	tokens := testTokens(t)
	repo := &stubPostRepo{originalKeys: []string{"originals/7/a.jpg", "originals/7/b.jpg"}}
	st := &stubStorage{}
	h := newRouter(t, testDeps{posts: repo, storage: st, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), token))
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿が %d で返った。201 を期待した: %s", rec.Code, rec.Body.String())
	}

	if len(st.keptKeys) != 2 {
		t.Fatalf("保持印を付けたキーが %d 件だった。2 件を期待した: %v", len(st.keptKeys), st.keptKeys)
	}
	for i, want := range []string{"originals/7/a.jpg", "originals/7/b.jpg"} {
		if st.keptKeys[i] != want {
			t.Errorf("%d件目が %q だった。%q を期待した", i+1, st.keptKeys[i], want)
		}
	}
}

// 保持印が付けられなくても投稿は成立している。
// **ここで 500 を返すと「投稿はできているのにエラーが出る」形になる。**
// 記録は残るが、利用者から見た結果は変えない。
func Test保持印を付けられなくても投稿は成功する(t *testing.T) {
	tokens := testTokens(t)
	st := &stubStorage{markKeptErr: errors.New("S3 に届かない")}
	h := newRouter(t, testDeps{storage: st, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), token))
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿が %d で返った。201 を期待した: %s", rec.Code, rec.Body.String())
	}
}
