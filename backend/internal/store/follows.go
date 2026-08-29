package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// フォローに関する store のエラー。
var (
	ErrUserNotFoundByHandle = errors.New("利用者が見つからない")
	ErrCannotFollowSelf     = errors.New("自分自身はフォローできない")
)

// FollowStore はフォローとプロフィールを扱う。
//
// *sql.DB を保持しているのは、フォローの登録・解除で通知の作成・削除を
// 同一トランザクションで行うためである（いいねと同じ理由）。
type FollowStore struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewFollowStore(db *sql.DB) *FollowStore {
	return &FollowStore{db: db, q: dbgen.New(db)}
}

// ---------------------------------------------------------------------------
// プロフィール
// ---------------------------------------------------------------------------

// UserProfile はプロフィール画面に出す情報。
type UserProfile struct {
	User                   domain.User
	PostCount              int
	FollowingCount         int
	FollowerCount          int
	VisitedPrefectureCount int
	IsFollowing            bool
}

// FindUserByHandle はハンドルで利用者を引く。
//
// 退会済みは対象外である（クエリ側で deleted_at を見ている）。
func (s *FollowStore) FindUserByHandle(ctx context.Context, handle string) (domain.User, error) {
	row, err := s.q.GetUserByHandle(ctx, handle)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrUserNotFoundByHandle
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("利用者の取得に失敗した: %w", err)
	}
	return domain.User{
		ID:          row.ID,
		Handle:      row.Handle,
		DisplayName: row.DisplayName,
		Bio:         nullStringToPtr(row.Bio),
	}, nil
}

// Profile はプロフィールに必要な集計をまとめて返す。
//
// **件数はカウンタ列にしていない。** プロフィールは1画面につき1回しか開かれず、
// フィードのように20件分を繰り返し引く経路が無い。いいね数とは事情が違う。
func (s *FollowStore) Profile(ctx context.Context, handle string, viewerID uint64) (UserProfile, error) {
	user, err := s.FindUserByHandle(ctx, handle)
	if err != nil {
		return UserProfile{}, err
	}

	posts, err := s.q.CountPostsByUser(ctx, user.ID)
	if err != nil {
		return UserProfile{}, fmt.Errorf("投稿数を取得できない: %w", err)
	}
	following, err := s.q.CountFollowing(ctx, user.ID)
	if err != nil {
		return UserProfile{}, fmt.Errorf("フォロー数を取得できない: %w", err)
	}
	followers, err := s.q.CountFollowers(ctx, user.ID)
	if err != nil {
		return UserProfile{}, fmt.Errorf("フォロワー数を取得できない: %w", err)
	}
	prefectures, err := s.q.CountVisitedPrefectures(ctx, user.ID)
	if err != nil {
		return UserProfile{}, fmt.Errorf("訪問した都道府県の数を取得できない: %w", err)
	}

	// 自分自身のプロフィールでは問い合わせない。結果は必ず false であり、
	// 自分をフォローすることはできない。
	var isFollowing bool
	if viewerID != user.ID {
		isFollowing, err = s.q.IsFollowing(ctx, dbgen.IsFollowingParams{
			FollowerID: viewerID,
			FolloweeID: user.ID,
		})
		if err != nil {
			return UserProfile{}, fmt.Errorf("フォローの状態を取得できない: %w", err)
		}
	}

	return UserProfile{
		User:                   user,
		PostCount:              int(posts),
		FollowingCount:         int(following),
		FollowerCount:          int(followers),
		VisitedPrefectureCount: int(prefectures),
		IsFollowing:            isFollowing,
	}, nil
}

// PrefectureCounts は都道府県ごとの投稿数を47件すべて返す。
//
// **プロフィールと同じ入口から引けるようにしている。** 制覇マップは
// プロフィールの一部であり、呼び出し側が2つの store を持たずに済む。
func (s *FollowStore) PrefectureCounts(ctx context.Context, userID uint64) ([]domain.PrefectureCount, error) {
	rows, err := s.q.ListPrefectureCountsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("都道府県ごとの投稿数を取得できない: %w", err)
	}

	out := make([]domain.PrefectureCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.PrefectureCount{
			Code:      r.Code,
			Name:      r.Name,
			Region:    r.Region,
			PostCount: int(r.PostCount),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// フォロー
// ---------------------------------------------------------------------------

// Follow はフォローする。既にフォローしていれば何もしない（冪等）。
//
// **登録と通知の作成を1つのトランザクションで行う。** 分かれていると
// 「フォローはされたが通知が来ない」状態が生じる。
func (s *FollowStore) Follow(ctx context.Context, followerID, followeeID uint64) error {
	// **DB の CHECK 制約でも防いでいるが、ここでも判定する。**
	// 制約違反をそのまま返すと 500 になり、利用者に理由が伝わらない。
	if followerID == followeeID {
		return ErrCannotFollowSelf
	}

	return inTx(ctx, s.db, s.q, func(q *dbgen.Queries) error {
		res, err := q.InsertFollow(ctx, dbgen.InsertFollowParams{
			FollowerID: followerID,
			FolloweeID: followeeID,
		})
		if err != nil {
			return fmt.Errorf("フォローの登録に失敗した: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("フォローの登録結果を確認できない: %w", err)
		}
		// **既にフォローしていた場合は 0 件になる。** 通知を作り直さない。
		if n == 0 {
			return nil
		}

		return createNotification(ctx, q, followeeID, followerID, dbgen.NotificationsTypeFollow, nil, nil)
	})
}

// Unfollow はフォローを解除する。フォローしていなければ何もしない（冪等）。
func (s *FollowStore) Unfollow(ctx context.Context, followerID, followeeID uint64) error {
	if followerID == followeeID {
		return nil
	}

	return inTx(ctx, s.db, s.q, func(q *dbgen.Queries) error {
		res, err := q.DeleteFollow(ctx, dbgen.DeleteFollowParams{
			FollowerID: followerID,
			FolloweeID: followeeID,
		})
		if err != nil {
			return fmt.Errorf("フォローの解除に失敗した: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("解除の結果を確認できない: %w", err)
		}
		if n == 0 {
			return nil
		}

		// 解除したら通知も消す。残すと「フォローされた」通知だけが宙に浮く。
		if err := q.DeleteFollowNotification(ctx, dbgen.DeleteFollowNotificationParams{
			UserID:  followeeID,
			ActorID: followerID,
		}); err != nil {
			return fmt.Errorf("通知の削除に失敗した: %w", err)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// 一覧
// ---------------------------------------------------------------------------

// ListFollowers はその利用者をフォローしている人を返す。
//
// 並びは利用者 id の昇順である（フォローした順ではない）。索引が
// (followee_id, follower_id) であり、この順でのみ索引だけで続きから取れる。
func (s *FollowStore) ListFollowers(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.User, uint64, error) {
	rows, err := s.q.ListFollowersAfter(ctx, dbgen.ListFollowersAfterParams{
		FolloweeID: userID,
		FollowerID: cursorID,
		Limit:      int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("フォロワーの取得に失敗した: %w", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, r := range rows {
		users = append(users, domain.User{
			ID:          r.ID,
			Handle:      r.Handle,
			DisplayName: r.DisplayName,
			Bio:         nullStringToPtr(r.Bio),
		})
	}
	return paginateUsers(users, limit)
}

// ListFollowing はその利用者がフォローしている人を返す。
func (s *FollowStore) ListFollowing(ctx context.Context, userID, cursorID uint64, limit int) ([]domain.User, uint64, error) {
	rows, err := s.q.ListFollowingAfter(ctx, dbgen.ListFollowingAfterParams{
		FollowerID: userID,
		FolloweeID: cursorID,
		Limit:      int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("フォロー中の取得に失敗した: %w", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, r := range rows {
		users = append(users, domain.User{
			ID:          r.ID,
			Handle:      r.Handle,
			DisplayName: r.DisplayName,
			Bio:         nullStringToPtr(r.Bio),
		})
	}
	return paginateUsers(users, limit)
}

// FollowedUserIDs は一覧に並んだ相手のうち、閲覧者がフォローしているものを返す。
//
// **一覧の1件ごとに問い合わせない。** 50件返す画面で50回の往復になる。
func (s *FollowStore) FollowedUserIDs(ctx context.Context, viewerID uint64, userIDs []uint64) (map[uint64]bool, error) {
	if len(userIDs) == 0 {
		return map[uint64]bool{}, nil
	}
	rows, err := s.q.ListFollowedUserIDs(ctx, dbgen.ListFollowedUserIDsParams{
		FollowerID: viewerID,
		UserIds:    userIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("フォローの状態を取得できない: %w", err)
	}
	out := make(map[uint64]bool, len(rows))
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 共通処理
// ---------------------------------------------------------------------------

// paginateUsers は「限度より1件多く取った結果」を1ページ分に切り、
// 続きがあれば次のカーソルを返す。
func paginateUsers(users []domain.User, limit int) ([]domain.User, uint64, error) {
	if len(users) <= limit {
		return users, 0, nil
	}
	users = users[:limit]
	return users, users[len(users)-1].ID, nil
}
