package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// 投稿に関する store のエラー。
var (
	ErrPostNotFound = errors.New("投稿が見つからない")

	// ErrMediaNotUsable は指定された画像を投稿に使えないことを表す。
	//
	// 他人の画像・処理が終わっていない画像・既に別の投稿に使われている画像を
	// 区別せず1つのエラーにしている。**区別して返すと、他人の画像の ID を
	// 総当たりして「存在するか」「処理済みか」を調べられる**ためである。
	ErrMediaNotUsable = errors.New("指定された画像を投稿に使えない")
)

// PostStore は投稿と画像を扱う。
type PostStore struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewPostStore(db *sql.DB) *PostStore {
	return &PostStore{db: db, q: dbgen.New(db)}
}

// CreatePendingMedia は「アップロードされる予定」を記録する。
//
// 署名付き URL を発行する前にこれを呼ぶ。先に記録しておかないと、
// 送信後に投稿が確定されなかったオブジェクトを後から特定できず、
// S3 に誰も参照しないデータが溜まり続ける。
func (s *PostStore) CreatePendingMedia(ctx context.Context, userID uint64, s3Key string) (uint64, error) {
	res, err := s.q.CreatePendingMedia(ctx, dbgen.CreatePendingMediaParams{UserID: userID, S3Key: s3Key})
	if err != nil {
		return 0, fmt.Errorf("画像の記録に失敗した: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("記録した画像のIDを取得できない: %w", err)
	}
	return uint64(id), nil
}

// CreatePostInput は投稿の作成に必要な入力。
type CreatePostInput struct {
	UserID         uint64
	Body           string
	PrefectureCode string
	SpotName       *string
	VisitedOn      *time.Time
	Tags           []string
	Media          []MediaAttachment
}

// MediaAttachment は投稿に紐づける画像1枚の指定。
type MediaAttachment struct {
	MediaID uint64
}

// CreatePost は投稿を作成し、画像とタグを紐づける。
//
// **全体を1つのトランザクションで行う。** 分かれていると、途中で失敗したときに
// 「画像の無い投稿」や「どの投稿にも属さない画像」が残る。
func (s *PostStore) CreatePost(ctx context.Context, in CreatePostInput) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("トランザクションを開始できない: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := s.q.WithTx(tx)

	res, err := q.CreatePost(ctx, dbgen.CreatePostParams{
		UserID:         in.UserID,
		Body:           in.Body,
		PrefectureCode: in.PrefectureCode,
		SpotName:       ptrToNullString(in.SpotName),
		VisitedOn:      timeToNullTime(in.VisitedOn),
	})
	if err != nil {
		return 0, fmt.Errorf("投稿の作成に失敗した: %w", err)
	}
	postID64, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("作成した投稿のIDを取得できない: %w", err)
	}
	postID := uint64(postID64)

	if err := attachMedia(ctx, q, postID, in.UserID, in.Media); err != nil {
		return 0, err
	}
	if err := replaceTags(ctx, q, postID, in.Tags); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("投稿を確定できない: %w", err)
	}
	return postID, nil
}

// attachMedia は画像を投稿へ紐づける。
//
// 事前に SELECT で「使える画像か」を確認しない。確認と UPDATE の間に
// 別のリクエストが同じ画像を使う余地が残るためである。
// **条件を UPDATE の WHERE 句に入れ、更新件数で判定する。**
func attachMedia(ctx context.Context, q *dbgen.Queries, postID, userID uint64, media []MediaAttachment) error {
	for i, m := range media {
		res, err := q.AttachMediaToPost(ctx, dbgen.AttachMediaToPostParams{
			PostID:    sql.NullInt64{Int64: int64(postID), Valid: true},
			SortOrder: uint8(i),
			ID:        m.MediaID,
			UserID:    userID,
		})
		if err != nil {
			return fmt.Errorf("画像の紐づけに失敗した: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("画像の紐づけ結果を確認できない: %w", err)
		}
		if n == 0 {
			// 他人の画像 / 未処理 / 既に使用済み のいずれか。区別しない。
			return ErrMediaNotUsable
		}
	}
	return nil
}

// replaceTags は投稿のタグを入れ替える。
func replaceTags(ctx context.Context, q *dbgen.Queries, postID uint64, tags []string) error {
	if err := q.DetachAllTagsFromPost(ctx, postID); err != nil {
		return fmt.Errorf("タグの解除に失敗した: %w", err)
	}
	for _, name := range tags {
		res, err := q.UpsertTag(ctx, name)
		if err != nil {
			return fmt.Errorf("タグの登録に失敗した: %w", err)
		}
		tagID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("タグのIDを取得できない: %w", err)
		}
		if err := q.AttachTagToPost(ctx, dbgen.AttachTagToPostParams{
			PostID: postID,
			TagID:  uint64(tagID),
		}); err != nil {
			return fmt.Errorf("タグの紐づけに失敗した: %w", err)
		}
	}
	return nil
}

// UpdatePostInput は投稿の編集に必要な入力。
type UpdatePostInput struct {
	PostID         uint64
	UserID         uint64
	Body           string
	PrefectureCode string
	SpotName       *string
	VisitedOn      *time.Time
	Tags           []string
}

// UpdatePost は投稿を編集する。画像の差し替えはできない。
func (s *PostStore) UpdatePost(ctx context.Context, in UpdatePostInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("トランザクションを開始できない: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := s.q.WithTx(tx)

	// user_id を条件に入れているため、他人の投稿は更新されない。
	// 権限確認は呼び出し側でも行うが、ここでも二重に閉じておく。
	if err := q.UpdatePost(ctx, dbgen.UpdatePostParams{
		Body:           in.Body,
		PrefectureCode: in.PrefectureCode,
		SpotName:       ptrToNullString(in.SpotName),
		VisitedOn:      timeToNullTime(in.VisitedOn),
		ID:             in.PostID,
		UserID:         in.UserID,
	}); err != nil {
		return fmt.Errorf("投稿の更新に失敗した: %w", err)
	}

	if err := replaceTags(ctx, q, in.PostID, in.Tags); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("投稿の更新を確定できない: %w", err)
	}
	return nil
}

// PostOwner は投稿の所有者を返す。権限確認のために本体を全部読む必要はない。
func (s *PostStore) PostOwner(ctx context.Context, postID uint64) (uint64, error) {
	owner, err := s.q.GetPostOwner(ctx, postID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPostNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("投稿の所有者を取得できない: %w", err)
	}
	return owner, nil
}

// DeletePost は投稿を削除し、消すべき S3 のキーを返す。
//
// **キーを先に集めてから削除する。** 削除後では紐づけが消えており、
// どのオブジェクトを消せばよいか分からなくなる。
// S3 の削除は呼び出し側が行う（この層はデータベースだけを扱う）。
func (s *PostStore) DeletePost(ctx context.Context, postID, userID uint64) ([]string, error) {
	keys, err := s.q.ListMediaKeysByPostID(ctx, dbgen.ListMediaKeysByPostIDParams{
		PostID:   sql.NullInt64{Int64: int64(postID), Valid: true},
		PostID_2: sql.NullInt64{Int64: int64(postID), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("画像のキーを取得できない: %w", err)
	}

	res, err := s.q.DeletePost(ctx, dbgen.DeletePostParams{ID: postID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("投稿の削除に失敗した: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("削除結果を確認できない: %w", err)
	}
	if n == 0 {
		return nil, ErrPostNotFound
	}

	return keys, nil
}

// GetPost は投稿を取得する。
//
// 画像・変換物・タグをそれぞれ1クエリで取り、投稿ごとに N 回問い合わせない。
func (s *PostStore) GetPost(ctx context.Context, postID uint64, signer storage.URLSigner, urlTTL time.Duration) (domain.Post, error) {
	row, err := s.q.GetPostByID(ctx, postID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Post{}, ErrPostNotFound
	}
	if err != nil {
		return domain.Post{}, fmt.Errorf("投稿の取得に失敗した: %w", err)
	}

	mediaRows, err := s.q.ListMediaByPostID(ctx, sql.NullInt64{Int64: int64(postID), Valid: true})
	if err != nil {
		return domain.Post{}, fmt.Errorf("画像の取得に失敗した: %w", err)
	}
	variantRows, err := s.q.ListVariantsByPostID(ctx, sql.NullInt64{Int64: int64(postID), Valid: true})
	if err != nil {
		return domain.Post{}, fmt.Errorf("変換物の取得に失敗した: %w", err)
	}
	tags, err := s.q.ListTagsByPostID(ctx, postID)
	if err != nil {
		return domain.Post{}, fmt.Errorf("タグの取得に失敗した: %w", err)
	}

	// media_id ごとに変換物のキーを引けるようにする。
	keysByMedia := make(map[uint64]map[string]string, len(mediaRows))
	for _, v := range variantRows {
		if keysByMedia[v.MediaID] == nil {
			keysByMedia[v.MediaID] = map[string]string{}
		}
		keysByMedia[v.MediaID][string(v.Kind)] = v.S3Key
	}

	media := make([]domain.PostMedia, 0, len(mediaRows))
	for _, m := range mediaRows {
		thumb, medium, err := signVariants(ctx, signer, keysByMedia[m.ID], urlTTL)
		if err != nil {
			return domain.Post{}, err
		}
		media = append(media, domain.PostMedia{
			ID:        m.ID,
			Width:     int(m.Width.Int32),
			Height:    int(m.Height.Int32),
			ThumbURL:  thumb,
			MediumURL: medium,
		})
	}

	return domain.Post{
		ID: row.ID,
		Author: domain.User{
			ID:          row.UserID,
			Handle:      row.Handle,
			DisplayName: row.DisplayName,
			Bio:         nullStringToPtr(row.Bio),
		},
		Body: row.Body,
		Prefecture: domain.Prefecture{
			Code:     row.PrefectureCode,
			Name:     row.PrefectureName,
			NameKana: row.PrefectureNameKana,
			Region:   row.Region,
		},
		SpotName:     nullStringToPtr(row.SpotName),
		VisitedOn:    nullTimeToPtr(row.VisitedOn),
		Media:        media,
		Tags:         tags,
		LikeCount:    int(row.LikeCount),
		CommentCount: int(row.CommentCount),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// signVariants は thumb / medium の表示用 URL を作る。
//
// 変換物が無い場合は空文字を返す。処理が完了していない画像が
// 投稿に紐づくことは無い設計だが、**表示側を落とさない**ようにしておく。
func signVariants(ctx context.Context, signer storage.URLSigner, keys map[string]string, ttl time.Duration) (thumb, medium string, err error) {
	for kind, dst := range map[string]*string{"thumb": &thumb, "medium": &medium} {
		key, ok := keys[kind]
		if !ok {
			continue
		}
		url, err := signer.PresignGet(ctx, key, ttl)
		if err != nil {
			return "", "", fmt.Errorf("%s の表示用URLを発行できない: %w", kind, err)
		}
		*dst = url
	}
	return thumb, medium, nil
}

func ptrToNullString(v *string) sql.NullString {
	if v == nil || *v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}
