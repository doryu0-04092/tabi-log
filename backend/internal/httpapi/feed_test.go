package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

// feedPaths は同じ組み立てを通る3つの一覧。
//
// 件数とカーソルの検証、いいねの状態の一括取得、応答の形はすべて共通の
// 処理を通る。**3つとも同じように振る舞うことを、まとめて確かめる。**
// 片方だけ直し忘れたときにここが落ちる。
func feedPaths() map[string]string {
	return map[string]string{
		"新着":       "/api/posts",
		"フォロー中":    "/api/feed/following",
		"利用者ごとの投稿": "/api/users/traveler/posts",
	}
}

func TestFeedListingsShareValidation(t *testing.T) {
	for name, base := range feedPaths() {
		t.Run(name, func(t *testing.T) {
			tokens := testTokens(t)
			h := newRouter(t, testDeps{
				posts:   &stubPostRepo{},
				follows: &stubFollowRepo{users: testUsers()},
				tokens:  tokens,
			})
			token := mustIssue(t, tokens, 7)

			for _, q := range []string{
				"?cursor=abc",
				"?limit=0",
				fmt.Sprintf("?limit=%d", maxFeedLimit+1),
			} {
				rec := doJSON(h, withBearer(req(http.MethodGet, base+q, ""), token))
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("%s%s が %d で通った。400 を期待した", base, q, rec.Code)
				}
			}
		})
	}
}

func TestFeedListingsRequireAuthentication(t *testing.T) {
	for name, base := range feedPaths() {
		t.Run(name, func(t *testing.T) {
			h := newRouter(t, testDeps{
				posts:   &stubPostRepo{},
				follows: &stubFollowRepo{users: testUsers()},
			})

			rec := doJSON(h, req(http.MethodGet, base, ""))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s が %d を返した。401 を期待した", base, rec.Code)
			}
		})
	}
}

func TestFeedListingsReturnSameShape(t *testing.T) {
	for name, base := range feedPaths() {
		t.Run(name, func(t *testing.T) {
			tokens := testTokens(t)
			h := newRouter(t, testDeps{
				posts: &stubPostRepo{
					posts:      []domain.Post{{ID: 5, Author: domain.User{ID: 9, Handle: "other"}}},
					nextCursor: 5,
				},
				follows: &stubFollowRepo{users: testUsers()},
				tokens:  tokens,
			})

			rec := doJSON(h, withBearer(req(http.MethodGet, base, ""), mustIssue(t, tokens, 7)))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s が %d を返した。200 を期待した。body=%s", base, rec.Code, rec.Body.String())
			}

			var got struct {
				Data struct {
					Posts []struct {
						Id      int64 `json:"id"`
						IsLiked bool  `json:"isLiked"`
					} `json:"posts"`
					NextCursor *string `json:"nextCursor"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
			}
			if len(got.Data.Posts) != 1 || got.Data.Posts[0].Id != 5 {
				t.Fatalf("投稿が返っていない: %+v", got.Data.Posts)
			}
			// いいねの状態は投稿ごとではなく、一覧まるごとに1回引く。
			if got.Data.Posts[0].IsLiked {
				t.Fatal("いいねしていないのに isLiked=true を返している")
			}
			if got.Data.NextCursor == nil || *got.Data.NextCursor != "5" {
				t.Fatalf("nextCursor が正しくない: %v", got.Data.NextCursor)
			}
		})
	}
}

// **フォロー中フィードは閲覧者ごとに違う。** 誰の分を引くかを取り違えると、
// 他人のフィードが見えることになる。
func TestListFollowingFeedUsesViewer(t *testing.T) {
	posts := &stubPostRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: posts, tokens: tokens})

	doJSON(h, withBearer(req(http.MethodGet, "/api/feed/following", ""), mustIssue(t, tokens, 42)))

	if posts.lastViewerID != 42 {
		t.Fatalf("viewerID が %d で引かれている。42 を期待した", posts.lastViewerID)
	}
}

func TestListFollowingFeedEmpty(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: &stubPostRepo{}, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/feed/following", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した", rec.Code)
	}

	var got struct {
		Data struct {
			Posts      []struct{} `json:"posts"`
			NextCursor *string    `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	// **空でも 200 と空の配列を返す。** null を返すと画面側で分岐が増える。
	if got.Data.Posts == nil {
		t.Fatal("空のときに posts が null になっている")
	}
	if len(got.Data.Posts) != 0 {
		t.Fatalf("投稿が %d 件返っている。0件を期待した", len(got.Data.Posts))
	}
	if got.Data.NextCursor != nil {
		t.Fatalf("続きが無いのに nextCursor が入っている: %v", *got.Data.NextCursor)
	}
}
