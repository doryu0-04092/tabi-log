package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// 反応に関する store のエラー。
var (
	ErrCommentNotFound = errors.New("コメントが見つからない")
)

// ReactionStore はいいねとコメントを扱う。
//
// *sql.DB を保持しているのは、いずれの操作もカウンタ列の更新と通知の作成を
// 同一トランザクションで行う必要があるためである。
type ReactionStore struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewReactionStore(db *sql.DB) *ReactionStore {
	return &ReactionStore{db: db, q: dbgen.New(db)}
}

// ---------------------------------------------------------------------------
// いいね
// ---------------------------------------------------------------------------

// Like はいいねする。既にいいねしていれば何もしない（冪等）。
//
// **いいねの登録・カウンタの増加・通知の作成を1つのトランザクションで行う。**
// 分かれていると「いいねは記録されたが件数がずれる」「いいねされたのに
// 通知が来ない」という、利用者から見て壊れた状態が生じる。
func (s *ReactionStore) Like(ctx context.Context, userID, postID uint64) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		owner, err := postOwner(ctx, q, postID)
		if err != nil {
			return err
		}

		res, err := q.InsertLike(ctx, dbgen.InsertLikeParams{UserID: userID, PostID: postID})
		if err != nil {
			return fmt.Errorf("いいねの登録に失敗した: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("いいねの登録結果を確認できない: %w", err)
		}
		// **既にいいねしていた場合は 0 件になる。** そのときは
		// カウンタを増やさない。増やすと二重に数えることになる。
		if n == 0 {
			return nil
		}

		if err := q.IncrementLikeCount(ctx, postID); err != nil {
			return fmt.Errorf("いいね数の更新に失敗した: %w", err)
		}

		// 自分の投稿へのいいねで自分に通知しない。
		if owner == userID {
			return nil
		}
		return createNotification(ctx, q, owner, userID, dbgen.NotificationsTypeLike, &postID, nil)
	})
}

// Unlike はいいねを取り消す。いいねしていなければ何もしない（冪等）。
func (s *ReactionStore) Unlike(ctx context.Context, userID, postID uint64) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		owner, err := postOwner(ctx, q, postID)
		if err != nil {
			return err
		}

		res, err := q.DeleteLike(ctx, dbgen.DeleteLikeParams{UserID: userID, PostID: postID})
		if err != nil {
			return fmt.Errorf("いいねの取り消しに失敗した: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("取り消し結果を確認できない: %w", err)
		}
		if n == 0 {
			return nil
		}

		if err := q.DecrementLikeCount(ctx, postID); err != nil {
			return fmt.Errorf("いいね数の更新に失敗した: %w", err)
		}

		// 取り消したら通知も消す。残すと「いいねされた」通知だけが宙に浮く。
		if owner == userID {
			return nil
		}
		if err := q.DeleteLikeNotification(ctx, dbgen.DeleteLikeNotificationParams{
			UserID:  owner,
			ActorID: userID,
			PostID:  sql.NullInt64{Int64: int64(postID), Valid: true},
		}); err != nil {
			return fmt.Errorf("通知の削除に失敗した: %w", err)
		}
		return nil
	})
}

// LikedPostIDs は指定した投稿のうち、その利用者がいいねしているものを返す。
//
// **投稿ごとに問い合わせない。** フィードは1画面で20件返すため、
// 投稿ごとに引くと20回の往復になる。
func (s *ReactionStore) LikedPostIDs(ctx context.Context, userID uint64, postIDs []uint64) (map[uint64]bool, error) {
	if len(postIDs) == 0 {
		return map[uint64]bool{}, nil
	}
	rows, err := s.q.ListLikedPostIDs(ctx, dbgen.ListLikedPostIDsParams{UserID: userID, PostIds: postIDs})
	if err != nil {
		return nil, fmt.Errorf("いいねの状態を取得できない: %w", err)
	}
	out := make(map[uint64]bool, len(rows))
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// コメント
// ---------------------------------------------------------------------------

// CreateComment はコメントを作成する。
func (s *ReactionStore) CreateComment(ctx context.Context, userID, postID uint64, body string) (uint64, error) {
	var commentID uint64

	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		owner, err := postOwner(ctx, q, postID)
		if err != nil {
			return err
		}

		res, err := q.CreateComment(ctx, dbgen.CreateCommentParams{
			PostID: postID,
			UserID: userID,
			Body:   body,
		})
		if err != nil {
			return fmt.Errorf("コメントの作成に失敗した: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("作成したコメントのIDを取得できない: %w", err)
		}
		commentID = uint64(id)

		if err := q.IncrementCommentCount(ctx, postID); err != nil {
			return fmt.Errorf("コメント数の更新に失敗した: %w", err)
		}

		if owner == userID {
			return nil
		}
		return createNotification(ctx, q, owner, userID, dbgen.NotificationsTypeComment, &postID, &commentID)
	})

	return commentID, err
}

// GetComment はコメントを1件返す。
//
// 作成直後に返す本文を組み立てるために使う。created_at はデータベースが
// 付けるため、挿入した側の値では代用できない。
func (s *ReactionStore) GetComment(ctx context.Context, commentID uint64) (domain.Comment, error) {
	row, err := s.q.GetCommentByID(ctx, commentID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Comment{}, ErrCommentNotFound
	}
	if err != nil {
		return domain.Comment{}, fmt.Errorf("コメントの取得に失敗した: %w", err)
	}
	return domain.Comment{
		ID: row.ID,
		Author: domain.User{
			ID:          row.UserID,
			Handle:      row.Handle,
			DisplayName: row.DisplayName,
			Bio:         nullStringToPtr(row.Bio),
		},
		Body:      row.Body,
		CreatedAt: row.CreatedAt,
	}, nil
}

// CommentPermission はコメントの削除権限を判定するための情報。
type CommentPermission struct {
	CommentID   uint64
	PostID      uint64
	CommentUser uint64
	PostOwner   uint64
}

// FindCommentPermission は削除の可否を判定する材料を返す。
//
// 判定そのものは上位層で行う。ここで「消せるか」まで決めると、
// 権限の規則がデータアクセス層に埋もれて追いにくくなる。
func (s *ReactionStore) FindCommentPermission(ctx context.Context, commentID uint64) (CommentPermission, error) {
	row, err := s.q.GetCommentByID(ctx, commentID)
	if errors.Is(err, sql.ErrNoRows) {
		return CommentPermission{}, ErrCommentNotFound
	}
	if err != nil {
		return CommentPermission{}, fmt.Errorf("コメントの取得に失敗した: %w", err)
	}

	owner, err := s.q.GetPostOwner(ctx, row.PostID)
	if err != nil {
		return CommentPermission{}, fmt.Errorf("投稿の所有者を取得できない: %w", err)
	}

	return CommentPermission{
		CommentID:   row.ID,
		PostID:      row.PostID,
		CommentUser: row.UserID,
		PostOwner:   owner,
	}, nil
}

// DeleteComment はコメントを削除する。権限の確認は呼び出し側で済ませておくこと。
func (s *ReactionStore) DeleteComment(ctx context.Context, commentID, postID uint64) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		res, err := q.DeleteCommentByID(ctx, commentID)
		if err != nil {
			return fmt.Errorf("コメントの削除に失敗した: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("削除結果を確認できない: %w", err)
		}
		if n == 0 {
			return ErrCommentNotFound
		}
		if err := q.DecrementCommentCount(ctx, postID); err != nil {
			return fmt.Errorf("コメント数の更新に失敗した: %w", err)
		}
		// 通知は notifications の外部キーが ON DELETE CASCADE のため連鎖して消える。
		return nil
	})
}

// ListComments はコメントを古い順に返す。
//
// nextCursor が 0 なら、それ以上のコメントは無い。
func (s *ReactionStore) ListComments(ctx context.Context, postID, cursorID uint64, limit int) ([]domain.Comment, uint64, error) {
	// 1件多く取って「続きがあるか」を判定する。
	rows, err := s.q.ListCommentsAfter(ctx, dbgen.ListCommentsAfterParams{
		PostID: postID,
		ID:     cursorID,
		Limit:  int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("コメントの取得に失敗した: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	out := make([]domain.Comment, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Comment{
			ID: r.ID,
			Author: domain.User{
				ID:          r.UserID,
				Handle:      r.Handle,
				DisplayName: r.DisplayName,
				Bio:         nullStringToPtr(r.Bio),
			},
			Body:      r.Body,
			CreatedAt: r.CreatedAt,
		})
	}

	var next uint64
	if hasMore && len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	return out, next, nil
}

// ---------------------------------------------------------------------------
// 共通処理
// ---------------------------------------------------------------------------

// inTx はトランザクションの開始・巻き戻し・確定をまとめる。
//
// 同じ定型が何度も出るため切り出している。巻き戻しの書き忘れは
// 接続を握ったまま離さない不具合になり、見つけにくい。
func (s *ReactionStore) inTx(ctx context.Context, fn func(*dbgen.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("トランザクションを開始できない: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("変更を確定できない: %w", err)
	}
	return nil
}

// postOwner は投稿の所有者を返す。存在しなければ ErrPostNotFound。
func postOwner(ctx context.Context, q *dbgen.Queries, postID uint64) (uint64, error) {
	owner, err := q.GetPostOwner(ctx, postID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPostNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("投稿の所有者を取得できない: %w", err)
	}
	return owner, nil
}

// createNotification は通知を1件作る。
func createNotification(
	ctx context.Context,
	q *dbgen.Queries,
	recipientID, actorID uint64,
	kind dbgen.NotificationsType,
	postID, commentID *uint64,
) error {
	params := dbgen.CreateNotificationParams{
		UserID:  recipientID,
		ActorID: actorID,
		Type:    kind,
	}
	if postID != nil {
		params.PostID = sql.NullInt64{Int64: int64(*postID), Valid: true}
	}
	if commentID != nil {
		params.CommentID = sql.NullInt64{Int64: int64(*commentID), Valid: true}
	}
	if err := q.CreateNotification(ctx, params); err != nil {
		return fmt.Errorf("通知の作成に失敗した: %w", err)
	}
	return nil
}
