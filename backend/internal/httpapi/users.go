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

	PrefectureCounts(ctx context.Context, userID uint64) ([]domain.PrefectureCount, error)

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
	if _, ok := UserIDFrom(r.Context()); !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	// **相手を先に引く。** 存在しない利用者に空の一覧を返すと、
	// ハンドルの打ち間違いと「まだ投稿が無い」を区別できない。
	user, err := h.repo.FindUserByHandle(r.Context(), handle)
	if errors.Is(err, store.ErrUserNotFoundByHandle) {
		writeError(w, r, http.StatusNotFound, "not_found", "利用者が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	cursor, ok := parseCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	writeFeedPage(w, r, h.likes, h.logger, params.Limit,
		func(ctx context.Context, limit int) ([]domain.Post, string, error) {
			posts, next, err := h.posts.ListUserPosts(ctx, user.ID, cursor, limit, h.storage, displayURLTTL)
			return posts, formatCursor(next), err
		})
}

// ListUserPrefectures は都道府県ごとの投稿数を47件すべて返す。
//
// **制覇マップのためのデータである。** 投稿が無い県も 0 件として含める。
// 返さないと、画面側で都道府県マスタと突き合わせる処理が要る。
func (h *userHandler) ListUserPrefectures(w http.ResponseWriter, r *http.Request, handle gen.Handle) {
	if _, ok := UserIDFrom(r.Context()); !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
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

	counts, err := h.repo.PrefectureCounts(r.Context(), user.ID)
	if err != nil {
		h.internalError(w, r, "都道府県ごとの投稿数を取得できない", err)
		return
	}

	items := make([]gen.PrefectureCount, 0, len(counts))
	for _, c := range counts {
		items = append(items, gen.PrefectureCount{
			Code:      c.Code,
			Name:      c.Name,
			Region:    c.Region,
			PostCount: c.PostCount,
		})
	}

	var body gen.PrefectureCountListResponse
	body.Data.Prefectures = items
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

	limit, ok := parseLimit(w, r, limitParam, defaultUserLimit, maxUserLimit)
	if !ok {
		return
	}
	cursor, ok := parseCursor(w, r, cursorParam)
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

func (h *userHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	attrs := []any{slog.String("request_id", RequestIDFrom(r.Context()))}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	h.logger.ErrorContext(r.Context(), msg, attrs...)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}
