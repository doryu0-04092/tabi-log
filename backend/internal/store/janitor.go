package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

/*
溜まり続けるものを定期的に片付ける。

**どれも放っておいても例外は出ない。** 行が増え、オブジェクトが増え、
費用と検索の重さだけが静かに悪化する。動かして気づくことはない。

対象は2つ。

  - 期限の切れたリフレッシュトークンの行
  - どこからも参照されていない画像（投稿にもアバターにも使われなかったもの）

`docs/audit-2026-08-31.md` M4 / M6。
*/

// JanitorStore は掃除に必要なデータベース操作をまとめる。
type JanitorStore struct {
	q *dbgen.Queries
}

func NewJanitorStore(db *sql.DB) *JanitorStore {
	return &JanitorStore{q: dbgen.New(db)}
}

// DeleteExpiredRefreshTokens は期限の切れたトークンの行を消し、消した件数を返す。
//
// **失効済みでも期限内のものは残す。** 盗用の検知は「失効済みトークンの
// 再提示」で判定しており、行を消すと再提示が「知らないトークン」に見えて
// 検知できなくなる。
func (s *JanitorStore) DeleteExpiredRefreshTokens(ctx context.Context, before time.Time, limit int32) (int64, error) {
	res, err := s.q.DeleteExpiredRefreshTokens(ctx, dbgen.DeleteExpiredRefreshTokensParams{
		ExpiresAt: before,
		Limit:     limit,
	})
	if err != nil {
		return 0, fmt.Errorf("期限切れトークンを消せない: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("削除件数を取得できない: %w", err)
	}
	return n, nil
}

// OrphanMedia はどこからも参照されていない画像1件と、それに属する S3 の鍵。
type OrphanMedia struct {
	MediaID uint64

	// Keys は原本と変換物の両方を含む。**原本だけでは足りない。**
	// 変換物はライフサイクルの対象外（消すと表示中の投稿が壊れるため
	// variants/ には期限削除を置いていない）で、ここでしか消えない。
	Keys []string
}

// ListOrphanMedia は参照されていない画像と、消すべき S3 の鍵を集める。
//
// **消す前に集める。** 行を消してからでは鍵を辿れない。
func (s *JanitorStore) ListOrphanMedia(ctx context.Context, before time.Time, limit int32) ([]OrphanMedia, error) {
	rows, err := s.q.ListOrphanMedia(ctx, dbgen.ListOrphanMediaParams{
		CreatedAt: before,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("参照されていない画像を取得できない: %w", err)
	}

	out := make([]OrphanMedia, 0, len(rows))
	for _, r := range rows {
		keys := []string{r.S3Key}
		variants, err := s.q.ListVariantKeysByMediaID(ctx, r.ID)
		if err != nil {
			return nil, fmt.Errorf("変換物の鍵を取得できない (media_id=%d): %w", r.ID, err)
		}
		out = append(out, OrphanMedia{MediaID: r.ID, Keys: append(keys, variants...)})
	}
	return out, nil
}

// DeleteMedia は画像の行を消す。変換物の行は外部キーの連鎖で消える。
func (s *JanitorStore) DeleteMedia(ctx context.Context, mediaID uint64) error {
	if err := s.q.DeleteMediaByID(ctx, mediaID); err != nil {
		return fmt.Errorf("画像の行を消せない (media_id=%d): %w", mediaID, err)
	}
	return nil
}

// PendingDeletion は消し損ねた S3 のオブジェクト1件。
type PendingDeletion struct {
	ID    uint64
	S3Key string
}

// ListPendingDeletions は消し損ねたオブジェクトを取り出す。
func (s *JanitorStore) ListPendingDeletions(ctx context.Context, limit int32) ([]PendingDeletion, error) {
	rows, err := s.q.ListPendingObjectDeletions(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("削除待ちのオブジェクトを取得できない: %w", err)
	}
	out := make([]PendingDeletion, 0, len(rows))
	for _, r := range rows {
		out = append(out, PendingDeletion{ID: r.ID, S3Key: r.S3Key})
	}
	return out, nil
}

// ForgetPendingDeletion は消せたオブジェクトの控えを消す。
func (s *JanitorStore) ForgetPendingDeletion(ctx context.Context, id uint64) error {
	if err := s.q.DeletePendingObjectDeletion(ctx, id); err != nil {
		return fmt.Errorf("削除待ちの控えを消せない (id=%d): %w", id, err)
	}
	return nil
}

// RecordDeletionAttempt は失敗した回数を数える。
//
// **回数を消さない。** 何度も失敗しているものは、S3 側に
// 消せない理由があるということであり、記録が判断材料になる。
func (s *JanitorStore) RecordDeletionAttempt(ctx context.Context, id uint64) error {
	if err := s.q.IncrementPendingObjectDeletionAttempts(ctx, id); err != nil {
		return fmt.Errorf("削除の試行回数を記録できない (id=%d): %w", id, err)
	}
	return nil
}
