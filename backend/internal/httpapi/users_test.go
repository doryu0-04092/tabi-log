package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

// 利用者 7 = traveler、9 = other。
func testUsers() map[string]domain.User {
	return map[string]domain.User{
		"traveler": {ID: 7, Handle: "traveler", DisplayName: "たびびと"},
		"other":    {ID: 9, Handle: "other", DisplayName: "別の人"},
	}
}

// ---------------------------------------------------------------------------
// プロフィール
// ---------------------------------------------------------------------------

func TestGetUserProfileReturnsCounts(t *testing.T) {
	repo := &stubFollowRepo{
		users: testUsers(),
		profile: store.UserProfile{
			User:                   domain.User{ID: 9, Handle: "other", DisplayName: "別の人"},
			PostCount:              12,
			FollowingCount:         3,
			FollowerCount:          4,
			VisitedPrefectureCount: 5,
			IsFollowing:            true,
		},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/other", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Handle                 string `json:"handle"`
			PostCount              int    `json:"postCount"`
			FollowingCount         int    `json:"followingCount"`
			FollowerCount          int    `json:"followerCount"`
			VisitedPrefectureCount int    `json:"visitedPrefectureCount"`
			IsFollowing            bool   `json:"isFollowing"`
			IsMe                   bool   `json:"isMe"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if got.Data.Handle != "other" || got.Data.PostCount != 12 {
		t.Fatalf("プロフィールが正しく返っていない: %+v", got.Data)
	}
	if got.Data.FollowingCount != 3 || got.Data.FollowerCount != 4 || got.Data.VisitedPrefectureCount != 5 {
		t.Fatalf("件数が正しく返っていない: %+v", got.Data)
	}
	if !got.Data.IsFollowing {
		t.Fatal("isFollowing が反映されていない")
	}
	// 閲覧者は 7、対象は 9 なので自分ではない。
	if got.Data.IsMe {
		t.Fatal("他人のプロフィールに isMe=true を返している")
	}
}

// **isMe は閲覧者ごとに変わる。** 同じ相手でも、自分で開けば true になる。
func TestGetUserProfileMarksSelf(t *testing.T) {
	repo := &stubFollowRepo{
		users: testUsers(),
		profile: store.UserProfile{
			User: domain.User{ID: 7, Handle: "traveler", DisplayName: "たびびと"},
		},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/traveler", ""), mustIssue(t, tokens, 7)))

	var got struct {
		Data struct {
			IsMe bool `json:"isMe"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if !got.Data.IsMe {
		t.Fatal("自分のプロフィールに isMe=true を返していない")
	}
}

func TestGetUserProfileUnknownHandleReturns404(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/nobody", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
}

func TestGetUserProfileRequiresAuthentication(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	h := newRouter(t, testDeps{follows: repo})

	rec := doJSON(h, req(http.MethodGet, "/api/users/traveler", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d が返った。401 を期待した", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// フォロー
// ---------------------------------------------------------------------------

func TestFollowUserIsIdempotent(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	// 同じ要求を2回送る。連打や再送で 409 を返す作りだと画面がエラーを出す。
	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodPut, "/api/users/other/follow", ""), token))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d回目で %d が返った。204 を期待した", i+1, rec.Code)
		}
	}
	if len(repo.followCalls) != 2 {
		t.Fatalf("repo の呼び出しが %d 回。2回を期待した", len(repo.followCalls))
	}
	if repo.followCalls[0] != [2]uint64{7, 9} {
		t.Fatalf("フォローの向きが %v。{7 9} を期待した", repo.followCalls[0])
	}
}

func TestUnfollowUserIsIdempotent(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodDelete, "/api/users/other/follow", ""), token))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d回目で %d が返った。204 を期待した", i+1, rec.Code)
		}
	}
	if len(repo.unfollowCalls) != 2 {
		t.Fatalf("repo の呼び出しが %d 回。2回を期待した", len(repo.unfollowCalls))
	}
}

// **自分自身はフォローできない。** DB の CHECK 制約でも防いでいるが、
// 制約違反を 500 として返すと利用者に理由が伝わらない。
func TestFollowSelfReturns400(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodPut, "/api/users/traveler/follow", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d が返った。400 を期待した。body=%s", rec.Code, rec.Body.String())
	}
}

func TestFollowUnknownHandleReturns404(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodPut, "/api/users/nobody/follow", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
	if len(repo.followCalls) != 0 {
		t.Fatalf("存在しない相手へのフォローが repo まで到達している: %v", repo.followCalls)
	}
}

func TestFollowRequiresAuthentication(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	h := newRouter(t, testDeps{follows: repo})

	rec := doJSON(h, req(http.MethodPut, "/api/users/other/follow", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d が返った。401 を期待した", rec.Code)
	}
	if len(repo.followCalls) != 0 {
		t.Fatalf("認証なしのリクエストが repo まで到達している: %v", repo.followCalls)
	}
}

// ---------------------------------------------------------------------------
// フォロー・フォロワーの一覧
// ---------------------------------------------------------------------------

func TestListFollowersMarksFollowState(t *testing.T) {
	repo := &stubFollowRepo{
		users: testUsers(),
		list: []domain.User{
			{ID: 9, Handle: "other", DisplayName: "別の人"},
			{ID: 7, Handle: "traveler", DisplayName: "たびびと"},
		},
		followed:   map[uint64]bool{9: true},
		nextCursor: 7,
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/traveler/followers", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Users []struct {
				Id          int64 `json:"id"`
				IsFollowing bool  `json:"isFollowing"`
				IsMe        bool  `json:"isMe"`
			} `json:"users"`
			NextCursor *string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if len(got.Data.Users) != 2 {
		t.Fatalf("利用者が %d 件。2件を期待した", len(got.Data.Users))
	}
	if !got.Data.Users[0].IsFollowing {
		t.Fatal("フォロー済みの相手に isFollowing=true を返していない")
	}
	// 閲覧者自身は一覧に混ざりうる。自分にフォローの導線を出さないため印を付ける。
	if !got.Data.Users[1].IsMe {
		t.Fatal("閲覧者自身に isMe=true を返していない")
	}
	if got.Data.NextCursor == nil || *got.Data.NextCursor != "7" {
		t.Fatalf("続きがあるのに nextCursor が正しくない: %v", got.Data.NextCursor)
	}
}

func TestListFollowingUsesGivenCursorAndLimit(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(
		req(http.MethodGet, "/api/users/traveler/following?cursor=42&limit=10", ""),
		mustIssue(t, tokens, 7),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastCursor != 42 || repo.lastLimit != 10 {
		t.Fatalf("cursor=%d limit=%d が渡っている。42 と 10 を期待した", repo.lastCursor, repo.lastLimit)
	}
}

func TestListFollowersRejectsBadCursorAndLimit(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for _, path := range []string{
		"/api/users/traveler/followers?cursor=abc",
		"/api/users/traveler/followers?limit=0",
		fmt.Sprintf("/api/users/traveler/followers?limit=%d", maxUserLimit+1),
	} {
		rec := doJSON(h, withBearer(req(http.MethodGet, path, ""), token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s が %d で通った。400 を期待した", path, rec.Code)
		}
	}
}

func TestListFollowersUnknownHandleReturns404(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/nobody/followers", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 利用者の投稿一覧
// ---------------------------------------------------------------------------

func TestListUserPostsReturnsPosts(t *testing.T) {
	follows := &stubFollowRepo{users: testUsers()}
	posts := &stubPostRepo{
		posts: []domain.Post{
			{ID: 100, Author: domain.User{ID: 9, Handle: "other"}, Body: "他人の投稿"},
		},
		nextCursor: 100,
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: follows, posts: posts, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/other/posts", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Posts []struct {
				Id        int64 `json:"id"`
				CanDelete bool  `json:"canDelete"`
			} `json:"posts"`
			NextCursor *string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if len(got.Data.Posts) != 1 || got.Data.Posts[0].Id != 100 {
		t.Fatalf("投稿が返っていない: %+v", got.Data.Posts)
	}
	// 閲覧者 7 は投稿者 9 ではないので消せない。
	if got.Data.Posts[0].CanDelete {
		t.Fatal("他人の投稿に canDelete=true を返している")
	}
	if got.Data.NextCursor == nil || *got.Data.NextCursor != "100" {
		t.Fatalf("nextCursor が正しくない: %v", got.Data.NextCursor)
	}
}

func TestListUserPostsUnknownHandleReturns404(t *testing.T) {
	follows := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: follows, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/nobody/posts", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 都道府県ごとの投稿数（制覇マップ）
// ---------------------------------------------------------------------------

// **投稿が無い県も返す。** 返さないと、画面側で都道府県マスタと
// 突き合わせる処理が要る。
func TestListUserPrefecturesIncludesZeroCounts(t *testing.T) {
	repo := &stubFollowRepo{
		users: testUsers(),
		prefectures: []domain.PrefectureCount{
			{Code: "01", Name: "北海道", Region: "北海道", PostCount: 2},
			{Code: "13", Name: "東京都", Region: "関東", PostCount: 0},
		},
	}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/traveler/prefectures", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Prefectures []struct {
				Code      string `json:"code"`
				Name      string `json:"name"`
				Region    string `json:"region"`
				PostCount int    `json:"postCount"`
			} `json:"prefectures"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if len(got.Data.Prefectures) != 2 {
		t.Fatalf("都道府県が %d 件。2件を期待した", len(got.Data.Prefectures))
	}
	if got.Data.Prefectures[0].PostCount != 2 || got.Data.Prefectures[0].Name != "北海道" {
		t.Fatalf("件数と名前が正しくない: %+v", got.Data.Prefectures[0])
	}
	// 0件の県が落ちていないこと。
	if got.Data.Prefectures[1].PostCount != 0 || got.Data.Prefectures[1].Code != "13" {
		t.Fatalf("0件の県が返っていない: %+v", got.Data.Prefectures[1])
	}
}

func TestListUserPrefecturesUnknownHandleReturns404(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{follows: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/users/nobody/prefectures", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}
}

func TestListUserPrefecturesRequiresAuthentication(t *testing.T) {
	repo := &stubFollowRepo{users: testUsers()}
	h := newRouter(t, testDeps{follows: repo})

	rec := doJSON(h, req(http.MethodGet, "/api/users/traveler/prefectures", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d が返った。401 を期待した", rec.Code)
	}
}
