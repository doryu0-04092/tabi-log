package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

const (
	maxDisplayNameRunes = 50
	maxBioRunes         = 300
)

// AccountRepository は自分自身の設定に関する操作を表す。
type AccountRepository interface {
	Current(ctx context.Context, userID uint64) (domain.User, error)
	UpdateProfile(ctx context.Context, userID uint64, displayName string, bio *string) (domain.User, error)
	Credentials(ctx context.Context, userID uint64) (string, error)
	ChangePassword(ctx context.Context, userID uint64, newHash string, now time.Time) error
	DeleteAccount(ctx context.Context, userID uint64, now time.Time) ([]string, error)

	AvatarRepository
}

type accountHandler struct {
	repo    AccountRepository
	posts   PostRepository
	likes   ReactionRepository
	follows FollowRepository
	storage ObjectStorage
	avatars *avatarResolver
	opts    AuthOptions
	logger  *slog.Logger
	now     func() time.Time
}

// ---------------------------------------------------------------------------
// プロフィールの編集
// ---------------------------------------------------------------------------

func (h *accountHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	var req gen.UpdateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// **現在の値を起点にする。** 送られなかった項目は変えない。
	current, err := h.repo.Current(r.Context(), userID)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}
	if err != nil {
		h.internalError(w, r, "現在のプロフィールを取得できない", err)
		return
	}

	displayName := current.DisplayName
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if n := utf8.RuneCountInString(displayName); n < 1 || n > maxDisplayNameRunes {
			writeError(w, r, http.StatusBadRequest, "validation_error",
				"表示名は1〜50文字にしてください")
			return
		}
	}

	bio := current.Bio
	// **省略は「変えない」、空文字は「消す」。**
	// null と省略を復号後に区別するには生の本文を見る必要があり、
	// 仕組みが増えるわりに得るものが無い。
	if req.Bio != nil {
		trimmed := strings.TrimSpace(*req.Bio)
		if utf8.RuneCountInString(trimmed) > maxBioRunes {
			writeError(w, r, http.StatusBadRequest, "validation_error",
				"自己紹介は300文字以内にしてください")
			return
		}
		if trimmed == "" {
			bio = nil
		} else {
			bio = &trimmed
		}
	}

	updated, err := h.repo.UpdateProfile(r.Context(), userID, displayName, bio)
	if err != nil {
		h.internalError(w, r, "プロフィールの更新に失敗した", err)
		return
	}

	out := toAPIUser(updated)
	h.avatars.fillOne(r.Context(), &out)
	writeJSON(w, r, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// パスワードの変更
// ---------------------------------------------------------------------------

func (h *accountHandler) ChangePassword(w http.ResponseWriter, r *http.Request, _ gen.ChangePasswordParams) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	var req gen.ChangePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// **長さの検証は auth パッケージに寄せる。** 登録時と同じ規則
	// （バイト数で数える。bcrypt の 72 バイト制限に気づけるようにするため）
	// を使わないと、登録できるのに変更できないパスワードが生まれる。
	if err := auth.ValidatePasswordLength(req.NewPassword); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error",
			"パスワードは8バイト以上72バイト以下にしてください")
		return
	}

	if !h.verifyCurrentPassword(w, r, userID, req.CurrentPassword) {
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		h.internalError(w, r, "パスワードをハッシュ化できない", err)
		return
	}

	// **変更と全トークンの失効は store 側で1つのトランザクションになる。**
	// 分かれていると、パスワードは変わったが古いセッションが生き残る。
	if err := h.repo.ChangePassword(r.Context(), userID, hash, h.now()); err != nil {
		h.internalError(w, r, "パスワードの変更に失敗した", err)
		return
	}

	// 呼び出した側のトークンも失効しているため、Cookie を消して入り直させる。
	http.SetCookie(w, h.expiredRefreshCookie())
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// 退会
// ---------------------------------------------------------------------------

func (h *accountHandler) DeleteAccount(w http.ResponseWriter, r *http.Request, _ gen.DeleteAccountParams) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	var req gen.DeleteAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// **取り消せない操作なので、本人であることを確かめる。**
	if !h.verifyCurrentPassword(w, r, userID, req.CurrentPassword) {
		return
	}

	keys, err := h.repo.DeleteAccount(r.Context(), userID, h.now())
	if err != nil {
		h.internalError(w, r, "退会の処理に失敗した", err)
		return
	}

	// **S3 は外部キーの連鎖では消えない。** データベース側を確定させてから消す。
	// ここで失敗しても退会は成立させる。「データベースは消えたが S3 に残る」は
	// 棚卸しで拾えるが、逆は表示が壊れる。
	if len(keys) > 0 {
		if err := h.storage.Delete(r.Context(), keys...); err != nil {
			h.logger.ErrorContext(r.Context(), "退会時の画像削除に失敗した",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.Uint64("user_id", userID),
				slog.Int("keys", len(keys)),
				slog.String("error", err.Error()),
			)
		}
	}

	http.SetCookie(w, h.expiredRefreshCookie())
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// 旅行履歴
// ---------------------------------------------------------------------------

func (h *accountHandler) ListUserTravels(w http.ResponseWriter, r *http.Request, handle gen.Handle, params gen.ListUserTravelsParams) {
	if _, ok := UserIDFrom(r.Context()); !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	user, err := h.follows.FindUserByHandle(r.Context(), handle)
	if errors.Is(err, store.ErrUserNotFoundByHandle) {
		writeError(w, r, http.StatusNotFound, "not_found", "利用者が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	cursor, ok := parseTravelCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	writeFeedPage(w, r, h.likes, h.avatars, h.logger, params.Limit,
		func(ctx context.Context, limit int) ([]domain.Post, string, error) {
			posts, next, err := h.posts.ListUserTravels(ctx, user.ID, cursor, limit, h.storage, displayURLTTL)
			return posts, formatTravelCursor(next), err
		})
}

// travelCursorSeparator は旅行履歴のカーソルの区切り。
const travelCursorSeparator = "_"

// parseTravelCursor は "<訪問日>_<投稿ID>" を読む。
//
// **訪問日は重複する。** 日付だけでは同じ日の中のどこまで返したかを
// 表せないため、投稿 ID と組にしている。
func parseTravelCursor(w http.ResponseWriter, r *http.Request, param *string) (store.TravelCursor, bool) {
	// 未指定なら「一番上から」。訪問日は未来日を受け付けないため、
	// 十分に先の日付を上限に置ける。
	start := store.TravelCursor{
		VisitedOn: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
		ID:        ^uint64(0),
	}
	if param == nil || *param == "" {
		return start, true
	}

	invalid := func() (store.TravelCursor, bool) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "カーソルの指定が不正です")
		return store.TravelCursor{}, false
	}

	date, idPart, found := strings.Cut(*param, travelCursorSeparator)
	if !found {
		return invalid()
	}
	visitedOn, err := time.Parse("2006-01-02", date)
	if err != nil {
		return invalid()
	}
	id, err := strconv.ParseUint(idPart, 10, 64)
	if err != nil {
		return invalid()
	}
	return store.TravelCursor{VisitedOn: visitedOn, ID: id}, true
}

func formatTravelCursor(c store.TravelCursor) string {
	if c.ID == 0 {
		return ""
	}
	return c.VisitedOn.Format("2006-01-02") + travelCursorSeparator + strconv.FormatUint(c.ID, 10)
}

// ---------------------------------------------------------------------------
// 共通処理
// ---------------------------------------------------------------------------

// verifyCurrentPassword は現在のパスワードを照合する。
//
// 合わないときは 400 で断る。**401 にしない。** ログイン状態そのものは
// 有効であり、401 を返すと画面がログイン画面へ飛ばしてしまう。
func (h *accountHandler) verifyCurrentPassword(w http.ResponseWriter, r *http.Request, userID uint64, password string) bool {
	hash, err := h.repo.Credentials(r.Context(), userID)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return false
	}
	if err != nil {
		h.internalError(w, r, "利用者を取得できない", err)
		return false
	}

	if !auth.VerifyPassword(hash, password) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "現在のパスワードが正しくありません")
		return false
	}
	return true
}

// expiredRefreshCookie は Cookie を消すための Set-Cookie を作る。
func (h *accountHandler) expiredRefreshCookie() *http.Cookie {
	return &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.opts.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
}

func (h *accountHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	attrs := []any{slog.String("request_id", RequestIDFrom(r.Context()))}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	h.logger.ErrorContext(r.Context(), msg, attrs...)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}
