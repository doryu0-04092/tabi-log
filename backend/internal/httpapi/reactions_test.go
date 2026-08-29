package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

// doJSON はリクエストを1本流し、記録用のレスポンスを返す。
func doJSON(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func req(method, path, body string) *http.Request {
	if body == "" {
		return httptest.NewRequest(method, path, nil)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// ---------------------------------------------------------------------------
// いいね
// ---------------------------------------------------------------------------

func TestLikePostRequiresAuthentication(t *testing.T) {
	repo := &stubReactionRepo{}
	h := newRouter(t, testDeps{reactions: repo})

	rec := doJSON(h, req(http.MethodPut, "/api/posts/1/likes", ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("認証なしで %d が返った。401 を期待した", rec.Code)
	}
	if len(repo.likeCalls) != 0 {
		t.Fatalf("認証なしのリクエストが repo まで到達している: %v", repo.likeCalls)
	}
}

func TestLikePostIsIdempotent(t *testing.T) {
	repo := &stubReactionRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	// **同じ要求を2回送る。** 連打や再送で 409 を返す作りだと、
	// 画面が理由もなくエラーを出すことになる。
	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodPut, "/api/posts/42/likes", ""), token))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d回目で %d が返った。204 を期待した", i+1, rec.Code)
		}
	}

	if len(repo.likeCalls) != 2 {
		t.Fatalf("repo の呼び出しが %d 回。2回を期待した", len(repo.likeCalls))
	}
	if repo.likeCalls[0] != [2]uint64{7, 42} {
		t.Fatalf("利用者と投稿の組が %v。{7 42} を期待した", repo.likeCalls[0])
	}
}

func TestUnlikePostIsIdempotent(t *testing.T) {
	repo := &stubReactionRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodDelete, "/api/posts/42/likes", ""), token))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d回目で %d が返った。204 を期待した", i+1, rec.Code)
		}
	}
	if len(repo.unlikeCalls) != 2 {
		t.Fatalf("repo の呼び出しが %d 回。2回を期待した", len(repo.unlikeCalls))
	}
}

func TestLikePostUnknownPostReturns404(t *testing.T) {
	repo := &stubReactionRepo{likeErr: store.ErrPostNotFound}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodPut, "/api/posts/999/likes", ""), mustIssue(t, tokens, 1)))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("存在しない投稿で %d が返った。404 を期待した", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// コメントの作成
// ---------------------------------------------------------------------------

func TestCreateCommentReturnsCreatedComment(t *testing.T) {
	repo := &stubReactionRepo{
		createID: 31,
		comments: []domain.Comment{{
			ID:        31,
			Author:    domain.User{ID: 7, Handle: "tabi", DisplayName: "旅人"},
			Body:      "いい景色ですね",
			CreatedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		}},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodPost, "/api/posts/1/comments", `{"body":"いい景色ですね"}`),
		mustIssue(t, tokens, 7),
	))

	if rec.Code != http.StatusCreated {
		t.Fatalf("%d が返った。201 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			ID        int64  `json:"id"`
			Body      string `json:"body"`
			CanDelete bool   `json:"canDelete"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if got.Data.ID != 31 || got.Data.Body != "いい景色ですね" {
		t.Fatalf("作成したコメントが返っていない: %+v", got.Data)
	}
	// 作成者は自分なので、必ず消せる。
	if !got.Data.CanDelete {
		t.Fatal("自分のコメントの canDelete が false になっている")
	}
}

// **文字数はバイト数ではなく文字数で数える。**
// 日本語のコメントをバイト数で弾くと、利用者から見て
// 「短いのに長すぎると言われる」ことになる。
func TestCreateCommentCountsRunesNotBytes(t *testing.T) {
	repo := &stubReactionRepo{createID: 1}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

	// 全角500文字 = 1500バイト。上限ちょうどなので通らなければならない。
	body := strings.Repeat("あ", maxCommentRunes)
	repo.comments = []domain.Comment{{ID: 1, Author: domain.User{ID: 7}, Body: body}}

	rec := doJSON(h, withBearer(
		req(http.MethodPost, "/api/posts/1/comments", fmt.Sprintf(`{"body":%q}`, body)),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusCreated {
		t.Fatalf("全角%d文字が %d で拒否された。201 を期待した。body=%s", maxCommentRunes, rec.Code, rec.Body.String())
	}

	// 1文字超えたら拒否する。
	over := strings.Repeat("あ", maxCommentRunes+1)
	rec = doJSON(h, withBearer(
		req(http.MethodPost, "/api/posts/1/comments", fmt.Sprintf(`{"body":%q}`, over)),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("上限超過が %d で通った。400 を期待した", rec.Code)
	}
}

func TestCreateCommentRejectsWhitespaceOnly(t *testing.T) {
	repo := &stubReactionRepo{createID: 1}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodPost, "/api/posts/1/comments", `{"body":"   \n\t  "}`),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空白のみのコメントが %d で通った。400 を期待した", rec.Code)
	}
	if len(repo.created) != 0 {
		t.Fatalf("空白のみのコメントが保存されている: %q", repo.created)
	}
}

func TestCreateCommentTrimsSurroundingWhitespace(t *testing.T) {
	repo := &stubReactionRepo{createID: 1, comments: []domain.Comment{{ID: 1, Author: domain.User{ID: 7}}}}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

	doJSON(h, withBearer(
		req(http.MethodPost, "/api/posts/1/comments", `{"body":"  こんにちは  "}`),
		mustIssue(t, tokens, 7),
	))

	if len(repo.created) != 1 || repo.created[0] != "こんにちは" {
		t.Fatalf("前後の空白が取り除かれていない: %q", repo.created)
	}
}

// ---------------------------------------------------------------------------
// コメントの取得
// ---------------------------------------------------------------------------

func TestListCommentsMarksDeletableByPostOwner(t *testing.T) {
	// 投稿の所有者は 7。コメントの作成者は 9。
	repo := &stubReactionRepo{
		comments: []domain.Comment{
			{ID: 1, Author: domain.User{ID: 9, Handle: "other"}, Body: "他人のコメント"},
			{ID: 2, Author: domain.User{ID: 7, Handle: "me"}, Body: "自分のコメント"},
		},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, posts: &stubPostRepo{owner: 7}, tokens: tokens})

	// 閲覧者は投稿の所有者（7）。他人のコメントも消せなければならない。
	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/posts/1/comments", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Comments []struct {
				ID        int64 `json:"id"`
				CanDelete bool  `json:"canDelete"`
			} `json:"comments"`
			NextCursor *string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if len(got.Data.Comments) != 2 {
		t.Fatalf("コメントが %d 件。2件を期待した", len(got.Data.Comments))
	}
	for _, c := range got.Data.Comments {
		if !c.CanDelete {
			t.Fatalf("投稿の所有者がコメント %d を消せない扱いになっている", c.ID)
		}
	}
	if got.Data.NextCursor != nil {
		t.Fatalf("続きが無いのに nextCursor が入っている: %v", *got.Data.NextCursor)
	}
}

func TestListCommentsHidesDeleteFromUnrelatedViewer(t *testing.T) {
	repo := &stubReactionRepo{
		comments:   []domain.Comment{{ID: 1, Author: domain.User{ID: 9}}},
		nextCursor: 1,
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, posts: &stubPostRepo{owner: 7}, tokens: tokens})

	// 閲覧者 55 は投稿者でもコメント作成者でもない。
	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/posts/1/comments", ""), mustIssue(t, tokens, 55)))

	var got struct {
		Data struct {
			Comments []struct {
				CanDelete bool `json:"canDelete"`
			} `json:"comments"`
			NextCursor *string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if got.Data.Comments[0].CanDelete {
		t.Fatal("無関係の閲覧者に canDelete=true を返している")
	}
	if got.Data.NextCursor == nil || *got.Data.NextCursor != "1" {
		t.Fatalf("続きがあるのに nextCursor が正しくない: %v", got.Data.NextCursor)
	}
}

func TestListCommentsRejectsBadCursorAndLimit(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{posts: &stubPostRepo{owner: 7}, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for _, path := range []string{
		"/api/posts/1/comments?cursor=abc",
		"/api/posts/1/comments?limit=0",
		fmt.Sprintf("/api/posts/1/comments?limit=%d", maxCommentLimit+1),
	} {
		rec := doJSON(h, withBearer(req(http.MethodGet, path, ""), token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s が %d で通った。400 を期待した", path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// コメントの削除 — 認可
// ---------------------------------------------------------------------------

func TestDeleteCommentAuthorization(t *testing.T) {
	// コメント 5 は投稿 1（所有者 7）に付いた、利用者 9 のコメント。
	perm := store.CommentPermission{CommentID: 5, PostID: 1, CommentUser: 9, PostOwner: 7}

	tests := []struct {
		name   string
		viewer uint64
		want   int
	}{
		{"コメントの作成者は消せる", 9, http.StatusNoContent},
		{"投稿の所有者は消せる", 7, http.StatusNoContent},
		{"無関係の利用者は消せない", 55, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubReactionRepo{perm: perm}
			tokens := testTokens(t)
			h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

			rec := doJSON(h, withBearer(
				req(http.MethodDelete, "/api/comments/5", ""),
				mustIssue(t, tokens, tt.viewer),
			))
			if rec.Code != tt.want {
				t.Fatalf("%d が返った。%d を期待した。body=%s", rec.Code, tt.want, rec.Body.String())
			}

			// **拒否したときは削除が走っていないことまで確かめる。**
			// ステータスだけ見ていると、消したうえで 403 を返す実装を見逃す。
			if tt.want == http.StatusForbidden && len(repo.deleted) != 0 {
				t.Fatalf("403 を返しつつ削除している: %v", repo.deleted)
			}
			if tt.want == http.StatusNoContent && len(repo.deleted) != 1 {
				t.Fatalf("削除が呼ばれていない: %v", repo.deleted)
			}
		})
	}
}

func TestDeleteCommentUnknownReturns404(t *testing.T) {
	repo := &stubReactionRepo{permErr: store.ErrCommentNotFound}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{reactions: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodDelete, "/api/comments/999", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
}

func TestDeleteCommentRequiresAuthentication(t *testing.T) {
	repo := &stubReactionRepo{perm: store.CommentPermission{CommentID: 5, CommentUser: 9, PostOwner: 7}}
	h := newRouter(t, testDeps{reactions: repo})

	rec := doJSON(h, req(http.MethodDelete, "/api/comments/5", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d が返った。401 を期待した", rec.Code)
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("認証なしで削除が走っている: %v", repo.deleted)
	}
}
