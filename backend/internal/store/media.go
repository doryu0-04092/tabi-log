package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// ErrMediaNotFound は画像の記録が見つからないことを表す。
var ErrMediaNotFound = errors.New("画像の記録が見つからない")

// MediaRecord は画像処理に必要な最小限の情報。
type MediaRecord struct {
	ID     uint64
	UserID uint64
	S3Key  string
	Status string
}

// ProcessedMedia は処理が完了した画像の情報。
type ProcessedMedia struct {
	MediaID  uint64
	Mime     string
	Width    int
	Height   int
	Bytes    int
	Variants []ProcessedVariant
}

// ProcessedVariant は変換後の画像1つ。
type ProcessedVariant struct {
	Kind   string
	S3Key  string
	Width  int
	Height int
	Bytes  int
}

// FindMediaByS3Key は S3 のキーから記録を引く。
//
// 画像処理は S3 のイベントで起動するため、手がかりがキーしかない。
// **キーから記録を引けるということは、そのオブジェクトが
// このアプリケーションが発行した署名付き URL 経由で置かれたことを意味する。**
// 引けない場合は処理しない。
func (s *PostStore) FindMediaByS3Key(ctx context.Context, key string) (MediaRecord, error) {
	row, err := s.q.GetMediaByS3Key(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return MediaRecord{}, ErrMediaNotFound
	}
	if err != nil {
		return MediaRecord{}, fmt.Errorf("画像の記録を取得できない: %w", err)
	}
	return MediaRecord{ID: row.ID, UserID: row.UserID, S3Key: row.S3Key, Status: string(row.Status)}, nil
}

// CompleteMediaProcessing は変換物を登録し、画像を processed にする。
//
// **1つのトランザクションで行う。** 分かれていると、変換物が登録される前に
// processed になった画像が投稿に使われ、表示できない画像を含む投稿が生まれる。
func (s *PostStore) CompleteMediaProcessing(ctx context.Context, in ProcessedMedia) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("トランザクションを開始できない: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := s.q.WithTx(tx)

	for _, v := range in.Variants {
		if err := q.CreateMediaVariant(ctx, dbgen.CreateMediaVariantParams{
			MediaID: in.MediaID,
			Kind:    dbgen.MediaVariantsKind(v.Kind),
			S3Key:   v.S3Key,
			Width:   uint32(v.Width),
			Height:  uint32(v.Height),
			Bytes:   uint32(v.Bytes),
		}); err != nil {
			return fmt.Errorf("変換物の登録に失敗した: %w", err)
		}
	}

	if err := q.MarkMediaProcessed(ctx, dbgen.MarkMediaProcessedParams{
		Mime:   sql.NullString{String: in.Mime, Valid: true},
		Width:  sql.NullInt32{Int32: int32(in.Width), Valid: true},
		Height: sql.NullInt32{Int32: int32(in.Height), Valid: true},
		Bytes:  sql.NullInt32{Int32: int32(in.Bytes), Valid: true},
		ID:     in.MediaID,
	}); err != nil {
		return fmt.Errorf("画像の状態を更新できない: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("画像処理の完了を確定できない: %w", err)
	}
	return nil
}

// FailMediaProcessing は画像を failed にする。
//
// 記録を残すのは、投稿に使えない理由を利用者へ説明できるようにするためと、
// **黙って消えるより「失敗した」と分かる方が調査できる**ためである。
func (s *PostStore) FailMediaProcessing(ctx context.Context, mediaID uint64) error {
	if err := s.q.MarkMediaFailed(ctx, mediaID); err != nil {
		return fmt.Errorf("画像の失敗状態を記録できない: %w", err)
	}
	return nil
}

// FindMediaByID は画像の記録を返す。
//
// 所有者の確認は呼び出し側で行う。ここで user_id を条件に入れて
// 「見つからない」と返すこともできるが、**権限が無いのか存在しないのかを
// 呼び出し側が区別できなくなる**ため、判定材料を返す形にしている。
func (s *PostStore) FindMediaByID(ctx context.Context, mediaID uint64) (MediaRecord, error) {
	row, err := s.q.GetMediaByID(ctx, mediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return MediaRecord{}, ErrMediaNotFound
	}
	if err != nil {
		return MediaRecord{}, fmt.Errorf("画像の記録を取得できない: %w", err)
	}
	return MediaRecord{ID: row.ID, UserID: row.UserID, S3Key: row.S3Key, Status: string(row.Status)}, nil
}
