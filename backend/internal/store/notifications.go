package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// NotificationStore は通知の読み取りと既読化を扱う。
//
// **作成はここに無い。** 通知は契機となる操作（いいね・コメント・フォロー）と
// 同一トランザクションで作られるため、ReactionStore と FollowStore が
// それぞれの処理の中で行う。分けて呼べる形にすると、呼び忘れや
// 別トランザクションでの作成を許してしまう。
type NotificationStore struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewNotificationStore(db *sql.DB) *NotificationStore {
	return &NotificationStore{db: db, q: dbgen.New(db)}
}

// List は通知を新しい順に返す。
func (s *NotificationStore) List(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.Notification, uint64, error) {
	if cursorID == 0 {
		cursorID = maxCursorID
	}

	// 1件多く取って「続きがあるか」を判定する。
	rows, err := s.q.ListNotificationsBefore(ctx, dbgen.ListNotificationsBeforeParams{
		UserID: userID,
		ID:     cursorID,
		Limit:  int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("通知の取得に失敗した: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	out := make([]domain.Notification, 0, len(rows))
	for _, r := range rows {
		n := domain.Notification{
			ID:   r.ID,
			Kind: string(r.Type),
			Actor: domain.User{
				ID:          r.ActorID,
				Handle:      r.Handle,
				DisplayName: r.DisplayName,
				Bio:         nullStringToPtr(r.Bio),
			},
			IsRead:    r.ReadAt.Valid,
			CreatedAt: r.CreatedAt,
		}
		if r.PostID.Valid {
			id := uint64(r.PostID.Int64)
			n.PostID = &id
		}
		// コメントが消えていれば本文は無い。通知そのものも外部キーの連鎖で
		// 消えるため通常は起きないが、LEFT JOIN の結果を素直に扱う。
		if r.CommentBody.Valid {
			body := r.CommentBody.String
			n.CommentBody = &body
		}
		out = append(out, n)
	}

	var next uint64
	if hasMore && len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	return out, next, nil
}

// UnreadCount は未読の件数を返す。
func (s *NotificationStore) UnreadCount(ctx context.Context, userID uint64) (int, error) {
	n, err := s.q.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("未読の件数を取得できない: %w", err)
	}
	return int(n), nil
}

// MarkRead は1件を既読にする。既に既読なら何もしない（冪等）。
//
// **他人あての通知は既読にできない。** 見つからない場合と区別せず
// ErrNotificationNotFound を返す。区別すると、id を総当たりして
// 他人の通知の有無を調べられる。
func (s *NotificationStore) MarkRead(ctx context.Context, notificationID, userID uint64, now time.Time) error {
	exists, err := s.q.NotificationExists(ctx, dbgen.NotificationExistsParams{
		ID:     notificationID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("通知の存在を確認できない: %w", err)
	}
	if !exists {
		return ErrNotificationNotFound
	}

	// 既に既読なら 0 件になるが、それは失敗ではない。
	if _, err := s.q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{
		ReadAt: sql.NullTime{Time: now, Valid: true},
		ID:     notificationID,
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("通知の既読化に失敗した: %w", err)
	}
	return nil
}

// MarkAllRead はその利用者の未読をすべて既読にする。
func (s *NotificationStore) MarkAllRead(ctx context.Context, userID uint64, now time.Time) error {
	if err := s.q.MarkAllNotificationsRead(ctx, dbgen.MarkAllNotificationsReadParams{
		ReadAt: sql.NullTime{Time: now, Valid: true},
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("通知の一括既読化に失敗した: %w", err)
	}
	return nil
}
