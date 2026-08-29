package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

const (
	defaultNotificationLimit = 20
	maxNotificationLimit     = 50
)

// NotificationRepository は通知の読み取りと既読化を表す。
//
// **作成する操作を持たない。** 通知は契機となる操作と同一トランザクションで
// 作られるため、いいね・コメント・フォローの側が担う。ここから作れる形にすると、
// 別トランザクションでの作成を許してしまう。
type NotificationRepository interface {
	List(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.Notification, uint64, error)
	UnreadCount(ctx context.Context, userID uint64) (int, error)
	MarkRead(ctx context.Context, notificationID, userID uint64, now time.Time) error
	MarkAllRead(ctx context.Context, userID uint64, now time.Time) error
}

type notificationHandler struct {
	repo    NotificationRepository
	avatars *avatarResolver
	logger  *slog.Logger
	now     func() time.Time
}

func (h *notificationHandler) ListNotifications(w http.ResponseWriter, r *http.Request, params gen.ListNotificationsParams) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	limit, ok := parseLimit(w, r, params.Limit, defaultNotificationLimit, maxNotificationLimit)
	if !ok {
		return
	}
	cursor, ok := parseCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	items, next, err := h.repo.List(r.Context(), userID, cursor, limit)
	if err != nil {
		h.internalError(w, r, "通知の取得に失敗した", err)
		return
	}

	out := make([]gen.Notification, 0, len(items))
	for _, n := range items {
		out = append(out, toAPINotification(n))
	}

	actors := make([]*gen.User, 0, len(out))
	for i := range out {
		actors = append(actors, &out[i].Actor)
	}
	h.avatars.fill(r.Context(), actors)

	var body gen.NotificationListResponse
	body.Data.Notifications = out
	if next != 0 {
		s := strconv.FormatUint(next, 10)
		body.Data.NextCursor = &s
	}
	writeJSON(w, r, http.StatusOK, body.Data)
}

func (h *notificationHandler) GetUnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	count, err := h.repo.UnreadCount(r.Context(), userID)
	if err != nil {
		h.internalError(w, r, "未読の件数を取得できない", err)
		return
	}

	var body gen.UnreadCountResponse
	body.Data.UnreadCount = count
	writeJSON(w, r, http.StatusOK, body.Data)
}

func (h *notificationHandler) MarkNotificationRead(w http.ResponseWriter, r *http.Request, notificationID int64) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	err := h.repo.MarkRead(r.Context(), uint64(notificationID), userID, h.now())
	// **他人あての通知は「見つからない」として扱う。**
	// 403 と 404 を分けると、id を総当たりして他人の通知の有無を調べられる。
	if errors.Is(err, store.ErrNotificationNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "通知が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "通知の既読化に失敗した", err)
		return
	}

	// 既に既読でも 204。連打や再送で画面がエラーを出さないようにする。
	w.WriteHeader(http.StatusNoContent)
}

func (h *notificationHandler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	if err := h.repo.MarkAllRead(r.Context(), userID, h.now()); err != nil {
		h.internalError(w, r, "通知の一括既読化に失敗した", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *notificationHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	attrs := []any{slog.String("request_id", RequestIDFrom(r.Context()))}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	h.logger.ErrorContext(r.Context(), msg, attrs...)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}

func toAPINotification(n domain.Notification) gen.Notification {
	out := gen.Notification{
		Id:        int64(n.ID),
		Type:      gen.NotificationType(n.Kind),
		Actor:     toAPIUser(n.Actor),
		IsRead:    n.IsRead,
		CreatedAt: n.CreatedAt,
	}
	if n.PostID != nil {
		id := int64(*n.PostID)
		out.PostId = &id
	}
	out.CommentBody = n.CommentBody
	return out
}
