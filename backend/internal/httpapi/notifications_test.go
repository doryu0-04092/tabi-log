package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

func sampleNotifications() []domain.Notification {
	postID := uint64(100)
	body := "いい写真ですね"
	return []domain.Notification{
		{
			ID:          3,
			Kind:        "comment",
			Actor:       domain.User{ID: 9, Handle: "other", DisplayName: "別の人"},
			PostID:      &postID,
			CommentBody: &body,
			CreatedAt:   time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		},
		{
			ID:        2,
			Kind:      "like",
			Actor:     domain.User{ID: 9, Handle: "other", DisplayName: "別の人"},
			PostID:    &postID,
			IsRead:    true,
			CreatedAt: time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC),
		},
		{
			ID:        1,
			Kind:      "follow",
			Actor:     domain.User{ID: 9, Handle: "other", DisplayName: "別の人"},
			CreatedAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
		},
	}
}

// ---------------------------------------------------------------------------
// 一覧
// ---------------------------------------------------------------------------

func TestListNotificationsReturnsKindSpecificFields(t *testing.T) {
	repo := &stubNotificationRepo{items: sampleNotifications(), nextCursor: 1}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/notifications", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			Notifications []struct {
				Id          int64   `json:"id"`
				Type        string  `json:"type"`
				PostId      *int64  `json:"postId"`
				CommentBody *string `json:"commentBody"`
				IsRead      bool    `json:"isRead"`
			} `json:"notifications"`
			NextCursor *string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if len(got.Data.Notifications) != 3 {
		t.Fatalf("通知が %d 件。3件を期待した", len(got.Data.Notifications))
	}

	// **契機ごとに埋まる項目が違う。**
	// comment は本文まで返す（一覧のたびにコメントを取りに行かないため）。
	c := got.Data.Notifications[0]
	if c.Type != "comment" || c.PostId == nil || c.CommentBody == nil {
		t.Fatalf("コメントの通知に必要な項目が無い: %+v", c)
	}
	// like は投稿へ飛べればよく、本文は無い。
	l := got.Data.Notifications[1]
	if l.Type != "like" || l.PostId == nil || l.CommentBody != nil {
		t.Fatalf("いいねの通知の項目が期待と違う: %+v", l)
	}
	if !l.IsRead {
		t.Fatal("既読の通知に isRead=true を返していない")
	}
	// follow は投稿に紐づかない。
	f := got.Data.Notifications[2]
	if f.Type != "follow" || f.PostId != nil {
		t.Fatalf("フォローの通知に投稿が付いている: %+v", f)
	}

	if got.Data.NextCursor == nil || *got.Data.NextCursor != "1" {
		t.Fatalf("nextCursor が %v。\"1\" を期待した", got.Data.NextCursor)
	}
}

// **自分あてのものだけを引く。** 誰の分を引くかを取り違えると、
// 他人の通知が見えることになる。
func TestListNotificationsUsesViewer(t *testing.T) {
	repo := &stubNotificationRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})

	doJSON(h, withBearer(req(http.MethodGet, "/api/notifications", ""), mustIssue(t, tokens, 42)))

	if repo.lastUserID != 42 {
		t.Fatalf("userID が %d で引かれている。42 を期待した", repo.lastUserID)
	}
}

func TestListNotificationsEmpty(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: &stubNotificationRepo{}, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/notifications", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した", rec.Code)
	}

	var got struct {
		Data struct {
			Notifications []struct{} `json:"notifications"`
			NextCursor    *string    `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	// 空でも 200 と空の配列を返す。null だと画面側で分岐が増える。
	if got.Data.Notifications == nil {
		t.Fatal("空のときに notifications が null になっている")
	}
	if got.Data.NextCursor != nil {
		t.Fatalf("続きが無いのに nextCursor が入っている: %v", *got.Data.NextCursor)
	}
}

func TestListNotificationsRejectsBadCursorAndLimit(t *testing.T) {
	tokens := testTokens(t)
	h := newRouter(t, testDeps{tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for _, path := range []string{
		"/api/notifications?cursor=abc",
		"/api/notifications?limit=0",
		fmt.Sprintf("/api/notifications?limit=%d", maxNotificationLimit+1),
	} {
		rec := doJSON(h, withBearer(req(http.MethodGet, path, ""), token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s が %d で通った。400 を期待した", path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// 未読の件数
// ---------------------------------------------------------------------------

func TestUnreadCount(t *testing.T) {
	repo := &stubNotificationRepo{unread: 5}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodGet, "/api/notifications/unread-count", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d が返った。200 を期待した。body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data struct {
			UnreadCount int `json:"unreadCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v body=%s", err, rec.Body.String())
	}
	if got.Data.UnreadCount != 5 {
		t.Fatalf("未読が %d 件。5件を期待した", got.Data.UnreadCount)
	}
}

// ---------------------------------------------------------------------------
// 既読化
// ---------------------------------------------------------------------------

func TestMarkNotificationReadIsIdempotent(t *testing.T) {
	repo := &stubNotificationRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodPut, "/api/notifications/3/read", ""), token))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d回目で %d が返った。204 を期待した", i+1, rec.Code)
		}
	}
	if len(repo.markedIDs) != 2 || repo.markedIDs[0] != 3 {
		t.Fatalf("既読化の呼び出しが %v。3 を2回を期待した", repo.markedIDs)
	}
}

// **他人あての通知は「見つからない」として断る。**
// 403 と 404 を分けると、id を総当たりして他人の通知の有無を調べられる。
func TestMarkNotificationReadHidesOthersWithNotFound(t *testing.T) {
	repo := &stubNotificationRepo{markErr: store.ErrNotificationNotFound}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})

	rec := doJSON(h, withBearer(req(http.MethodPut, "/api/notifications/999/read", ""), mustIssue(t, tokens, 7)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d が返った。404 を期待した", rec.Code)
	}

	var got struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v", err)
	}
	// forbidden を返すと「あるが権限が無い」と分かってしまう。
	if got.Error.Code != "not_found" {
		t.Fatalf("エラーコードが %q。not_found を期待した", got.Error.Code)
	}
}

func TestMarkAllNotificationsReadIsIdempotent(t *testing.T) {
	repo := &stubNotificationRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})
	token := mustIssue(t, tokens, 7)

	for i := range 2 {
		rec := doJSON(h, withBearer(req(http.MethodPut, "/api/notifications/read", ""), token))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d回目で %d が返った。204 を期待した", i+1, rec.Code)
		}
	}
	if len(repo.markedAllFor) != 2 || repo.markedAllFor[0] != 7 {
		t.Fatalf("一括既読化の呼び出しが %v。7 を2回を期待した", repo.markedAllFor)
	}
}

// **一括既読の経路が、1件の既読化に化けていないこと。**
// /notifications/read と /notifications/{id}/read は形が似ている。
func TestMarkAllDoesNotHitSingleRoute(t *testing.T) {
	repo := &stubNotificationRepo{}
	tokens := testTokens(t)
	h := newRouter(t, testDeps{notifications: repo, tokens: tokens})

	doJSON(h, withBearer(req(http.MethodPut, "/api/notifications/read", ""), mustIssue(t, tokens, 7)))

	if len(repo.markedIDs) != 0 {
		t.Fatalf("一括既読が1件の既読化として扱われた: %v", repo.markedIDs)
	}
}

func TestNotificationsRequireAuthentication(t *testing.T) {
	repo := &stubNotificationRepo{}
	h := newRouter(t, testDeps{notifications: repo})

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/notifications"},
		{http.MethodGet, "/api/notifications/unread-count"},
		{http.MethodPut, "/api/notifications/read"},
		{http.MethodPut, "/api/notifications/3/read"},
	}

	for _, c := range cases {
		rec := doJSON(h, req(c.method, c.path, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s が %d を返した。401 を期待した", c.method, c.path, rec.Code)
		}
	}
	if len(repo.markedIDs) != 0 || len(repo.markedAllFor) != 0 {
		t.Fatal("認証なしのリクエストが repo まで到達している")
	}
}
