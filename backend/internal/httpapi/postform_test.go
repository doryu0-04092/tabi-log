package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

// ---------------------------------------------------------------------------
// 訪問日
// ---------------------------------------------------------------------------

// **任意になった。** nil と「ゼロ値の日時」を区別する。
// 値として持たせると「訪問日が無い」と「西暦1年に行った」の区別が付かない。
func TestOptionalDateDistinguishesNil(t *testing.T) {
	if got := optionalDate(nil); got != nil {
		t.Fatalf("未指定なのに %v が返った", got)
	}
	if got := optionalOpenapiDate(nil); got != nil {
		t.Fatalf("未指定なのに %v が返った", got)
	}

	d := mustDate(t, "2026-05-03")
	out := optionalOpenapiDate(&d)
	if out == nil || out.Format("2006-01-02") != "2026-05-03" {
		t.Fatalf("訪問日が正しく変換されていない: %v", out)
	}
}

// 訪問日の無い投稿でも応答は組み立てられる。
func TestFeedIncludesPostWithoutVisitedOn(t *testing.T) {
	posts := &stubPostRepo{
		posts: []domain.Post{{ID: 5, Author: domain.User{ID: 9, Handle: "other"}}},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: posts, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/posts", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Posts []struct {
				Id        int64   `json:"id"`
				VisitedOn *string `json:"visitedOn"`
			} `json:"posts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if len(got.Data.Posts) != 1 {
		t.Fatalf("投稿が %d 件。1件を期待した", len(got.Data.Posts))
	}
	// **null であって、ゼロ値の日付ではない。**
	if got.Data.Posts[0].VisitedOn != nil {
		t.Fatalf("訪問日が %q。null を期待した", *got.Data.Posts[0].VisitedOn)
	}
}

// ---------------------------------------------------------------------------
// 投稿者のフォロー状態
// ---------------------------------------------------------------------------

// **投稿カードからその場でフォローできるようにするために持たせている。**
// 一覧の人数ぶんをまとめて解決し、1件ずつ引かない。
func TestFeedFillsAuthorFollowState(t *testing.T) {
	posts := &stubPostRepo{
		posts: []domain.Post{
			{ID: 5, Author: domain.User{ID: 9, Handle: "other"}},
			{ID: 6, Author: domain.User{ID: 7, Handle: "me"}},
		},
	}
	follows := &stubFollowRepo{followed: map[uint64]bool{9: true}}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: posts, follows: follows, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/posts", ""), mustIssue(t, tokens, 7)))

	var got struct {
		Data struct {
			Posts []struct {
				Author struct {
					Id          int64 `json:"id"`
					IsFollowing *bool `json:"isFollowing"`
					IsMe        *bool `json:"isMe"`
				} `json:"author"`
			} `json:"posts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}

	first := got.Data.Posts[0].Author
	if first.IsFollowing == nil || !*first.IsFollowing {
		t.Fatalf("フォロー済みの投稿者に isFollowing=true を返していない: %+v", first)
	}
	if first.IsMe == nil || *first.IsMe {
		t.Fatalf("他人の投稿者に isMe=true を返している: %+v", first)
	}

	// **自分の投稿にはフォローの導線を出さないため、isMe で見分ける。**
	second := got.Data.Posts[1].Author
	if second.IsMe == nil || !*second.IsMe {
		t.Fatalf("自分の投稿者に isMe=true を返していない: %+v", second)
	}
}
