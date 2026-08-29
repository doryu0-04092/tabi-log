package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// AccountStore はプロフィールの編集・パスワード変更・退会を扱う。
//
// *sql.DB を持つのは、退会が複数の表にまたがる削除であり、
// **途中で止まると「投稿だけ残った退会者」ができる**ためである。
type AccountStore struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewAccountStore(db *sql.DB) *AccountStore {
	return &AccountStore{db: db, q: dbgen.New(db)}
}

// Current は現在の利用者を返す。
//
// **編集は「送られた項目だけを変える」ため、起点となる現在の値が要る。**
func (s *AccountStore) Current(ctx context.Context, userID uint64) (domain.User, error) {
	row, err := s.q.GetUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		// 退会済みもここに来る（クエリが deleted_at を見ている）。
		return domain.User{}, ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("利用者を取得できない: %w", err)
	}
	return domain.User{
		ID:          row.ID,
		Handle:      row.Handle,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Bio:         nullStringToPtr(row.Bio),
	}, nil
}

// UpdateProfile は表示名と自己紹介を更新し、更新後の利用者を返す。
func (s *AccountStore) UpdateProfile(ctx context.Context, userID uint64, displayName string, bio *string) (domain.User, error) {
	var nullBio sql.NullString
	if bio != nil {
		nullBio = sql.NullString{String: *bio, Valid: true}
	}

	if err := s.q.UpdateProfile(ctx, dbgen.UpdateProfileParams{
		DisplayName: displayName,
		Bio:         nullBio,
		ID:          userID,
	}); err != nil {
		return domain.User{}, fmt.Errorf("プロフィールの更新に失敗した: %w", err)
	}

	row, err := s.q.GetUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("更新後の利用者を取得できない: %w", err)
	}
	return domain.User{
		ID:          row.ID,
		Handle:      row.Handle,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Bio:         nullStringToPtr(row.Bio),
	}, nil
}

// ErrAvatarNotUsable は指定された画像をアバターに使えないことを表す。
//
// 他人の画像・処理が終わっていない画像・既に投稿で使われている画像を
// 区別せず1つのエラーにしている。**区別して返すと、他人の画像の ID を
// 総当たりして「存在するか」「処理済みか」を調べられる**ためである
// （投稿への紐づけと同じ理由）。
var ErrAvatarNotUsable = errors.New("指定された画像をアバターに使えない")

// SetAvatar はアバターを設定する。
//
// **条件は SQL 側に置いている。** SELECT で確かめてから UPDATE すると、
// その間に同じ画像が投稿へ紐づけられる余地が残る。
func (s *AccountStore) SetAvatar(ctx context.Context, userID, mediaID uint64) error {
	res, err := s.q.SetAvatar(ctx, dbgen.SetAvatarParams{
		AvatarMediaID: sql.NullInt64{Int64: int64(mediaID), Valid: true},
		ID:            userID,
		ID_2:          mediaID,
		UserID:        userID,
	})
	if err != nil {
		return fmt.Errorf("アバターの設定に失敗した: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("アバターの設定結果を確認できない: %w", err)
	}
	if n == 0 {
		// **既に同じ画像が設定されていても 0 件になる。**
		// 使えない場合と区別できないが、どちらも「変わらない」という
		// 結果は同じであり、呼び出し側は冪等に扱える。
		return ErrAvatarNotUsable
	}
	return nil
}

// ClearAvatar はアバターを外す。
func (s *AccountStore) ClearAvatar(ctx context.Context, userID uint64) error {
	if err := s.q.ClearAvatar(ctx, userID); err != nil {
		return fmt.Errorf("アバターの解除に失敗した: %w", err)
	}
	return nil
}

// AvatarKeys は利用者ごとのアバター画像（thumb）の鍵を返す。
//
// **一覧で1件ずつ引かない。** フィード20件それぞれに問い合わせると
// 20回の往復になる。いいねの状態と同じ考え方である。
func (s *AccountStore) AvatarKeys(ctx context.Context, userIDs []uint64) (map[uint64]string, error) {
	if len(userIDs) == 0 {
		return map[uint64]string{}, nil
	}
	rows, err := s.q.ListAvatarKeys(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("アバターの鍵を取得できない: %w", err)
	}
	out := make(map[uint64]string, len(rows))
	for _, r := range rows {
		out[r.UserID] = r.S3Key
	}
	return out, nil
}

// Credentials はパスワードの照合に使う現在のハッシュを返す。
//
// **照合そのものは上位層で行う。** bcrypt の比較を store に置くと、
// 認証の判断がデータアクセス層へ散らばる。
func (s *AccountStore) Credentials(ctx context.Context, userID uint64) (string, error) {
	row, err := s.q.GetUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		// 退会済みもここに来る（クエリが deleted_at を見ている）。
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("利用者を取得できない: %w", err)
	}
	return row.PasswordHash, nil
}

// ChangePassword はパスワードを変え、そのユーザーの全リフレッシュトークンを失効させる。
//
// **1つのトランザクションで行う。** 分かれていると、パスワードは変わったが
// 古いセッションが生き残る、という最も避けたい状態が生じる。
func (s *AccountStore) ChangePassword(ctx context.Context, userID uint64, newHash string, now time.Time) error {
	return inTx(ctx, s.db, s.q, func(q *dbgen.Queries) error {
		if err := q.UpdatePassword(ctx, dbgen.UpdatePasswordParams{
			PasswordHash: newHash,
			ID:           userID,
		}); err != nil {
			return fmt.Errorf("パスワードの更新に失敗した: %w", err)
		}
		if err := q.RevokeAllRefreshTokensForUser(ctx, dbgen.RevokeAllRefreshTokensForUserParams{
			RevokedAt: sql.NullTime{Time: now, Valid: true},
			UserID:    userID,
		}); err != nil {
			return fmt.Errorf("リフレッシュトークンの失効に失敗した: %w", err)
		}
		return nil
	})
}

// DeleteAccount は退会させ、消すべき S3 のオブジェクトの鍵を返す。
//
// **S3 は外部キーの連鎖では消えない。** 呼び出し側が返された鍵で
// 明示的に削除する。データベース側を先に確定させるのは、
// 「S3 は消えたがデータベースに残る」より「データベースは消えたが
// S3 に残る」ほうが害が小さいためである（後者はライフサイクルや
// 棚卸しで拾えるが、前者は表示が壊れる）。
func (s *AccountStore) DeleteAccount(ctx context.Context, userID uint64, now time.Time) ([]string, error) {
	var keys []string

	err := inTx(ctx, s.db, s.q, func(q *dbgen.Queries) error {
		// **削除の前に鍵を集める。** 消してからでは辿れない。
		found, err := q.ListS3KeysByUser(ctx, dbgen.ListS3KeysByUserParams{
			UserID:   userID,
			UserID_2: userID,
		})
		if err != nil {
			return fmt.Errorf("画像の鍵を取得できない: %w", err)
		}
		keys = found

		// 投稿を消すと、その投稿へのいいね・コメント・タグの紐づけ・
		// メディアは外部キーの連鎖で消える。**他人が自分の投稿に付けた
		// コメントもここで消える。**
		if err := q.DeletePostsByUser(ctx, userID); err != nil {
			return fmt.Errorf("投稿の削除に失敗した: %w", err)
		}
		// 他人の投稿に付けた自分のコメント・いいねは連鎖しない。個別に消す。
		//
		// **消す前にカウンタ列を減らす。** 行を消しても posts の
		// comment_count / like_count は自動では減らない。減らさないと、
		// 退会者がコメントしていた他人の投稿の件数がずれたまま残る。
		if err := q.DecrementCommentCountsForUser(ctx, dbgen.DecrementCommentCountsForUserParams{
			UserID:   userID,
			UserID_2: userID,
		}); err != nil {
			return fmt.Errorf("コメント数の更新に失敗した: %w", err)
		}
		if err := q.DeleteCommentsByUser(ctx, userID); err != nil {
			return fmt.Errorf("コメントの削除に失敗した: %w", err)
		}
		if err := q.DecrementLikeCountsForUser(ctx, userID); err != nil {
			return fmt.Errorf("いいね数の更新に失敗した: %w", err)
		}
		if err := q.DeleteLikesByUser(ctx, userID); err != nil {
			return fmt.Errorf("いいねの削除に失敗した: %w", err)
		}
		if err := q.DeleteFollowsByUser(ctx, dbgen.DeleteFollowsByUserParams{
			FollowerID: userID,
			FolloweeID: userID,
		}); err != nil {
			return fmt.Errorf("フォローの削除に失敗した: %w", err)
		}
		// 投稿に紐づかないメディア（アップロードしたが未確定のもの）が残る。
		if err := q.DeleteMediaByUser(ctx, userID); err != nil {
			return fmt.Errorf("画像の削除に失敗した: %w", err)
		}

		// **メールアドレスは復元不能な値に置き換える。** UNIQUE 制約があるため、
		// 置き換えないと本人が同じアドレスで登録し直せない。
		// パスワードのハッシュも使えない値にして、退会後にログインできなくする。
		if err := q.SoftDeleteUser(ctx, dbgen.SoftDeleteUserParams{
			DeletedAt:    sql.NullTime{Time: now, Valid: true},
			Email:        fmt.Sprintf("deleted-%d@invalid.example", userID),
			PasswordHash: "deleted",
			ID:           userID,
		}); err != nil {
			return fmt.Errorf("退会の処理に失敗した: %w", err)
		}

		if err := q.RevokeAllRefreshTokensForUser(ctx, dbgen.RevokeAllRefreshTokensForUserParams{
			RevokedAt: sql.NullTime{Time: now, Valid: true},
			UserID:    userID,
		}); err != nil {
			return fmt.Errorf("リフレッシュトークンの失効に失敗した: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return keys, nil
}
