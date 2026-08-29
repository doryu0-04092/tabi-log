package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/search"
)

// ---------------------------------------------------------------------------
// 投稿を探す
// ---------------------------------------------------------------------------

func TestSearchPostsPassesEveryFilter(t *testing.T) {
	repo := &stubSearchRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, tokens: tokens})

	path := "/api/search/posts?q=函館&prefectureCode=01&region=北海道&tag=海鮮" +
		"&handle=traveler&visitedFrom=2026-01-01&visitedTo=2026-12-31&since=2026-08-01&sort=popular"
	rec := doJSON(h, withBearer(req(http.MethodGet, path, ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	f := repo.lastFilters
	if f.Keyword != "函館" || f.PrefectureCode != "01" || f.Region != "北海道" || f.Tag != "海鮮" {
		t.Fatalf("絞り込みが渡っていない: %+v", f)
	}
	if f.Handle != "traveler" {
		t.Fatalf("投稿者が渡っていない: %+v", f)
	}
	if f.VisitedFrom == nil || f.VisitedTo == nil || f.Since == nil {
		t.Fatalf("日付が渡っていない: %+v", f)
	}
	if f.Sort != search.SortPopular {
		t.Fatalf("並び順が %q。popular を期待した", f.Sort)
	}
}

// 何も指定しなくても検索できる（新着と同じ結果になる）。
func TestSearchPostsWithoutFilters(t *testing.T) {
	repo := &stubSearchRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/search/posts", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastFilters.Sort != search.SortLatest {
		t.Fatalf("既定の並び順が %q。latest を期待した", repo.lastFilters.Sort)
	}
}

// **1文字のキーワードは ngram の索引に当たらない。**
// 黙って空を返すと「壊れている」と受け取られるため、理由を返す。
func TestSearchPostsRejectsSingleCharacterKeyword(t *testing.T) {
	repo := &stubSearchRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/search/posts?q=海", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d が返った。400 を期待した", rec.Code)
	}
	if rec.Body.String() == "" || !containsJSONMessage(t, rec.Body.Bytes(), "2文字以上") {
		t.Fatalf("理由が伝わらない応答: %s", rec.Body.String())
	}
}

// 範囲が逆さまなら結果は必ず空になる。空を返すより誤りだと伝える。
func TestSearchPostsRejectsReversedVisitedRange(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodGet, "/api/search/posts?visitedFrom=2026-12-31&visitedTo=2026-01-01", ""),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d が返った。400 を期待した。body=%s", rec.Code, rec.Body.String())
	}
}

// **検索が決めた並びが、応答でもそのまま保たれること。**
// 本体は IN 句で取るため、並べ直さないとデータベース側の都合の順になる。
func TestSearchPostsKeepsSearchOrder(t *testing.T) {
	repo := &stubSearchRepo{ids: []uint64{3, 1, 2}}
	posts := &stubPostRepo{
		posts: []domain.Post{
			{ID: 1, Author: domain.User{ID: 9}},
			{ID: 2, Author: domain.User{ID: 9}},
			{ID: 3, Author: domain.User{ID: 9}},
		},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, posts: posts, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/search/posts", ""), mustIssue(t, tokens, 7)))

	var got struct {
		Data struct {
			Posts []struct {
				Id int64 `json:"id"`
			} `json:"posts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	ids := make([]int64, 0, len(got.Data.Posts))
	for _, p := range got.Data.Posts {
		ids = append(ids, p.Id)
	}
	if len(ids) != 3 || ids[0] != 3 || ids[1] != 1 || ids[2] != 2 {
		t.Fatalf("並びが %v。[3 1 2] を期待した", ids)
	}
}

// ---------------------------------------------------------------------------
// カーソル
// ---------------------------------------------------------------------------

// **並び順によってカーソルの形が違う。**
// 新着は "<id>"、人気順は "<いいね数>_<id>"。
func TestSearchPostsCursorShapeDependsOnSort(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		next       search.Cursor
		wantCursor string
	}{
		{"新着", "", search.Cursor{ID: 12}, "12"},
		{"人気順", "&sort=popular", search.Cursor{LikeCount: 5, ID: 12}, "5_12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubSearchRepo{ids: []uint64{1}, nextCursor: tt.next}
			posts := &stubPostRepo{posts: []domain.Post{{ID: 1, Author: domain.User{ID: 9}}}}
			tokens := testTokens(t)
			h := newRouter(t, testDeps{search: repo, posts: posts, tokens: tokens})

			rec := doJSON(h, withBearer(
				req(http.MethodGet, "/api/search/posts?limit=1"+tt.query, ""),
				mustIssue(t, tokens, 7),
			))

			var got struct {
				Data struct {
					NextCursor *string `json:"nextCursor"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
			}
			if got.Data.NextCursor == nil || *got.Data.NextCursor != tt.wantCursor {
				t.Fatalf("nextCursor が %v。%q を期待した", got.Data.NextCursor, tt.wantCursor)
			}
		})
	}
}

func TestSearchPostsParsesPopularCursor(t *testing.T) {
	repo := &stubSearchRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodGet, "/api/search/posts?sort=popular&cursor=5_12", ""),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastCursor.LikeCount != 5 || repo.lastCursor.ID != 12 {
		t.Fatalf("カーソルが %+v。{5 12} を期待した", repo.lastCursor)
	}
}

// 並び順を変えたまま前のカーソルを渡すと形が合わない。
// 黙って先頭から返すと「同じ投稿がまた出てくる」ことになる。
func TestSearchPostsRejectsMismatchedCursor(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for _, path := range []string{
		"/api/search/posts?sort=popular&cursor=12",  // 人気順に新着のカーソル
		"/api/search/posts?cursor=5_12",             // 新着に人気順のカーソル
		"/api/search/posts?sort=popular&cursor=a_b", // そもそも数でない
	} {
		rec := doJSON(h, withBearer(req(http.MethodGet, path, ""), token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s が %d で通った。400 を期待した", path, rec.Code)
		}
	}
}

// 先頭のページでは「一番上から」を表す上限値で引く。
func TestSearchPostsStartsFromTop(t *testing.T) {
	repo := &stubSearchRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, tokens: tokens})

	doJSON(h, withBearer(req(http.MethodGet, "/api/search/posts?sort=popular", ""), mustIssue(t, tokens, 7)))

	if repo.lastCursor.ID != ^uint64(0) || repo.lastCursor.LikeCount != ^uint32(0) {
		t.Fatalf("先頭のカーソルが %+v。上限値を期待した", repo.lastCursor)
	}
}

// ---------------------------------------------------------------------------
// 利用者を探す
// ---------------------------------------------------------------------------

func TestSearchUsersReturnsFollowState(t *testing.T) {
	repo := &stubSearchRepo{
		users: []domain.User{
			{ID: 9, Handle: "other", DisplayName: "別の人"},
			{ID: 7, Handle: "me", DisplayName: "自分"},
		},
	}
	follows := &stubFollowRepo{followed: map[uint64]bool{9: true}}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, follows: follows, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/search/users?q=ひと", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Users []struct {
				IsFollowing bool `json:"isFollowing"`
				IsMe        bool `json:"isMe"`
			} `json:"users"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if !got.Data.Users[0].IsFollowing {
		t.Fatal("フォロー済みの相手に isFollowing=true を返していない")
	}
	if !got.Data.Users[1].IsMe {
		t.Fatal("閲覧者自身に isMe=true を返していない")
	}
}

func TestSearchUsersTrimsAndRejectsShortKeyword(t *testing.T) {
	repo := &stubSearchRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{search: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/search/users?q=%20%20たび%20%20", ""), token))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastKeyword != "たび" {
		t.Fatalf("前後の空白が取り除かれていない: %q", repo.lastKeyword)
	}

	rec = doJSON(h, withBearer(req(http.MethodGet, "/api/search/users?q=%E3%81%82", ""), token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("1文字が %d で通った。400 を期待した", rec.Code)
	}
}

func TestSearchRequiresAuthentication(t *testing.T) {
	h := newRouter(t, testDeps{})

	for _, path := range []string{"/api/search/posts", "/api/search/users?q=たび"} {
		rec := doJSON(h, req(http.MethodGet, path, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s が %d を返した。401 を期待した", path, rec.Code)
		}
	}
}

// containsJSONMessage はエラー応答の message に語が含まれるかを見る。
func containsJSONMessage(t *testing.T, body []byte, want string) bool {
	t.Helper()
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return false
	}
	return strings.Contains(e.Error.Message, want)
}
