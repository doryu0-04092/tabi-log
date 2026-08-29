package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

/*
書き込みの上限。

**上限に達したときの振る舞いだけを見る。** 何回で達するかは設定であり、
値そのものを固定すると、上限を調整するたびにテストが落ちる。
*/

const createPostBody = `{"body":"上限の確認","prefectureCode":"01","visitedOn":null,"tags":[],"media":[{"mediaId":1}]}`

func Test投稿は上限を超えると429になる(t *testing.T) {
	tokens := testTokens(t)
	repo := &stubPostRepo{}
	h := newRouter(t, testDeps{posts: repo, tokens: tokens, postCreateLimit: 2})
	token := mustIssue(t, tokens, 7)

	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), token))
		if rec.Code != http.StatusCreated {
			t.Fatalf("%d回目が %d で返った。201 を期待した: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), token))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("上限を超えて %d が返った。429 を期待した: %s", rec.Code, rec.Body.String())
	}

	// **上限に達した分は保存まで届いていないこと。**
	// 429 を返しつつ書き込んでいては意味が無い。
	if len(repo.created) != 2 {
		t.Errorf("%d件が保存された。2件のはず", len(repo.created))
	}
}

// **鍵は利用者。** IP で数えると、同じ回線の別の利用者が巻き添えになる。
func Test投稿の上限は利用者ごとに数える(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens, postCreateLimit: 1})

	first := mustIssue(t, tokens, 7)
	second := mustIssue(t, tokens, 8)

	if rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), first)); rec.Code != http.StatusCreated {
		t.Fatalf("1人目の1回目が %d", rec.Code)
	}
	if rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), first)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("1人目の2回目が %d。429 を期待した", rec.Code)
	}

	// 別の利用者はまだ書ける。
	if rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), second)); rec.Code != http.StatusCreated {
		t.Fatalf("2人目が巻き添えになっている: %d", rec.Code)
	}
}

func Testコメントは上限を超えると429になる(t *testing.T) {
	tokens := testTokens(t)
	repo := &stubReactionRepo{
		createID: 1,
		comments: []domain.Comment{{
			ID:        1,
			Author:    domain.User{ID: 7, Handle: "tabi", DisplayName: "旅人"},
			Body:      "上限の確認",
			CreatedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		}},
	}
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens, commentCreateLimit: 2})
	token := mustIssue(t, tokens, 7)

	const body = `{"body":"上限の確認"}`
	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts/1/comments", body), token))
		if rec.Code != http.StatusCreated {
			t.Fatalf("%d回目が %d で返った。201 を期待した: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts/1/comments", body), token))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("上限を超えて %d が返った。429 を期待した: %s", rec.Code, rec.Body.String())
	}
}

// **読み取りと、いいね・フォローは数えない。**
// これらは押した回数がそのまま状態になるわけではなく（冪等）、
// 上限をかけると普通の操作を止めることになる。
func Test読み取りは上限の対象にしない(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens, postCreateLimit: 1})
	token := mustIssue(t, tokens, 7)

	for i := range 5 {
		rec := doJSON(h, withBearer(req(http.MethodGet, "/api/posts", ""), token))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d回目の読み取りが %d で返った", i+1, rec.Code)
		}
	}
}

// 未認証のリクエストは上限より先に 401 で止まる。
// **上限の記録が未認証の相手で埋まらない**ようにするためである。
func Test未認証は上限を消費しない(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens, postCreateLimit: 1})

	for range 3 {
		rec := doJSON(h, req(http.MethodPost, "/api/posts", createPostBody))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("認証なしで %d が返った。401 を期待した", rec.Code)
		}
	}

	rec := doJSON(h, withBearer(req(http.MethodPost, "/api/posts", createPostBody), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("認証済みの1回目が %d。201 を期待した: %s", rec.Code, rec.Body.String())
	}
}
