package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

const (
	defaultUserLimit = 50
	maxUserLimit     = 100
)

// FollowRepository はプロフィールとフォローの永続化操作を表す。
type FollowRepository interface {
	FindUserByHandle(ctx context.Context, handle string) (domain.User, error)
	Profile(ctx context.Context, handle string, viewerID uint64) (store.UserProfile, error)

	Follow(ctx context.Context, followerID, followeeID uint64) error
	Unfollow(ctx context.Context, followerID, followeeID uint64) error

	ListFollowers(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.User, uint64, error)
	ListFollowing(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.User, uint64, error)
	FollowedUserIDs(ctx context.Context, viewerID uint64, userIDs []uint64) (map[uint64]bool, error)
}

type userHandler struct {
	repo    FollowRepository
	posts   PostRepository
	likes   ReactionRepository
	storage ObjectStorage
	logger  *slog.Logger
}

// ---------------------------------------------------------------------------
// プロフィール
// ---------------------------------------------------------------------------

func (h *userHandler) GetUserProfile(w http.ResponseWriter, r *http.Request, handle gen.Handle) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	profile, err := h.repo.Profile(r.Context(), handle, viewerID)
	if errors.Is(err, store.ErrUserNotFoundByHandle) {
		writeError(w, r, http.StatusNotFound, "not_found", "利用者が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "プロフィールの取得に失敗した", err)
		return
	}

	writeJSON(w, r, http.StatusOK, gen.UserProfile{
		Id:                     int64(profile.User.ID),
		Handle:                 profile.User.Handle,
		DisplayName:            profile.User.DisplayName,
		Bio:                    profile.User.Bio,
		PostCount:              profile.PostCount,
		FollowingCount:         profile.FollowingCount,
		FollowerCount:          profile.FollowerCount,
		VisitedPrefectureCount: profile.VisitedPrefectureCount,
		IsFollowing:            profile.IsFollowing,
		IsMe:                   profile.User.ID == viewerID,
	})
}

func (h *userHandler) ListUserPosts(w http.ResponseWriter, r *http.Request, handle gen.Handle, params gen.ListUserPostsParams) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	limit, ok := h.resolveLimit(w, r, params.Limit, defaultFeedLimit, maxFeedLimit)
	if !ok {
		return
	}
	cursor, ok := h.resolveCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	user, err := h.repo.FindUserByHandle(r.Context(), handle)
	if errors.Is(err, store.ErrUserNotFoundByHandle) {
		writeError(w, r, http.StatusNotFound, "not_found", "利用者が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	posts, next, err := h.posts.ListUserPosts(r.Context(), user.ID, cursor, limit, h.storage, displayURLTTL)
	if err != nil {
		h.internalError(w, r, "投稿の取得に失敗した", err)
		return
	}

	// いいねの状態はフィードと同じくまとめて引く。
	postIDs := make([]uint64, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
	}
	liked, err := h.likes.LikedPostIDs(r.Context(), viewerID, postIDs)
	if err != nil {
		h.internalError(w, r, "いいねの状態を取得できない", err)
		return
	}

	items := make([]gen.Post, 0, len(posts))
	for _, p := range posts {
		items = append(items, toAPIPost(p, viewerID, liked[p.ID]))
	}

	var body gen.PostListResponse
	body.Data.Posts = items
	if next != 0 {
		s := strconv.FormatUint(next, 10)
		body.Data.NextCursor = &s
	}
	writeJSON(w, r, http.StatusOK, body.Data)
}

// ---------------------------------------------------------------------------
// フォロー
// ---------------------------------------------------------------------------

func (h *userHandler) FollowUser(w http.ResponseWriter, r *http.Request, handle gen.Handle) {
	h.toggleFollow(w, r, handle, true)
}

func (h *userHandler) UnfollowUser(w http.ResponseWriter, r *http.Request, handle gen.Handle) {
	h.toggleFollow(w, r, handle, false)
}

// toggleFollow はフォローの登録と解除をまとめる。
//
// どちらも「利用者を取り出す → 相手をハンドルで引く → 冪等に実行する」で
// 手順が同じであり、分けて書くと片方だけ直す間違いが起きる。
func (h *userHandler) toggleFollow(w http.ResponseWriter, r *http.Request, handle string, follow bool) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	target, err := h.repo.FindUserByHandle(r.Context(), handle)
	if errors.Is(err, store.ErrUserNotFoundByHandle) {
		writeError(w, r, http.StatusNotFound, "not_found", "利用者が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	if follow {
		err = h.repo.Follow(r.Context(), viewerID, target.ID)
	} else {
		err = h.repo.Unfollow(r.Context(), viewerID, target.ID)
	}

	if errors.Is(err, store.ErrCannotFollowSelf) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "自分自身はフォローできません")
		return
	}
	if err != nil {
		h.internalError(w, r, "フォローの処理に失敗した", err)
		return
	}

	// **どちらも冪等なので、状態が変わらなくても 204 を返す。**
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// フォロー・フォロワーの一覧
// ---------------------------------------------------------------------------

func (h *userHandler) ListFollowers(w http.ResponseWriter, r *http.Request, handle gen.Handle, params gen.ListFollowersParams) {
	h.listUsers(w, r, handle, params.Cursor, params.Limit, h.repo.ListFollowers)
}

func (h *userHandler) ListFollowing(w http.ResponseWriter, r *http.Request, handle gen.Handle, params gen.ListFollowingParams) {
	h.listUsers(w, r, handle, params.Cursor, params.Limit, h.repo.ListFollowing)
}

// listUsers はフォロワーとフォロー中の一覧をまとめる。
//
// 違いは引く向きだけなので、取得の関数を受け取る形にしてある。
func (h *userHandler) listUsers(
	w http.ResponseWriter,
	r *http.Request,
	handle string,
	cursorParam *string,
	limitParam *int,
	list func(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.User, uint64, error),
) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	limit, ok := h.resolveLimit(w, r, limitParam, defaultUserLimit, maxUserLimit)
	if !ok {
		return
	}
	cursor, ok := h.resolveCursor(w, r, cursorParam)
	if !ok {
		return
	}

	user, err := h.repo.FindUserByHandle(r.Context(), handle)
	if errors.Is(err, store.ErrUserNotFoundByHandle) {
		writeError(w, r, http.StatusNotFound, "not_found", "利用者が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	users, next, err := list(r.Context(), user.ID, cursor, limit)
	if err != nil {
		h.internalError(w, r, "一覧の取得に失敗した", err)
		return
	}

	// **一覧の1件ごとにフォローの状態を引かない。** まとめて1クエリで解決する。
	ids := make([]uint64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	followed, err := h.repo.FollowedUserIDs(r.Context(), viewerID, ids)
	if err != nil {
		h.internalError(w, r, "フォローの状態を取得できない", err)
		return
	}

	items := make([]gen.UserSummary, 0, len(users))
	for _, u := range users {
		items = append(items, gen.UserSummary{
			Id:          int64(u.ID),
			Handle:      u.Handle,
			DisplayName: u.DisplayName,
			Bio:         u.Bio,
			IsFollowing: followed[u.ID],
			IsMe:        u.ID == viewerID,
		})
	}

	var body gen.UserListResponse
	body.Data.Users = items
	if next != 0 {
		s := strconv.FormatUint(next, 10)
		body.Data.NextCursor = &s
	}
	writeJSON(w, r, http.StatusOK, body.Data)
}

// ---------------------------------------------------------------------------
// 共通処理
// ---------------------------------------------------------------------------

// resolveLimit は件数の指定を検証する。範囲外なら応答を書いて false を返す。
func (h *userHandler) resolveLimit(w http.ResponseWriter, r *http.Request, param *int, def, max int) (int, bool) {
	limit := def
	if param != nil {
		limit = *param
	}
	if limit < 1 || limit > max {
		writeError(w, r, http.StatusBadRequest, "validation_error", "取得件数の指定が不正です")
		return 0, false
	}
	return limit, true
}

// resolveCursor はカーソルの指定を検証する。省略時は 0（先頭から）。
func (h *userHandler) resolveCursor(w http.ResponseWriter, r *http.Request, param *string) (uint64, bool) {
	if param == nil || *param == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(*param, 10, 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "カーソルの指定が不正です")
		return 0, false
	}
	return v, true
}

func (h *userHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	attrs := []any{slog.String("request_id", RequestIDFrom(r.Context()))}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	h.logger.ErrorContext(r.Context(), msg, attrs...)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}
