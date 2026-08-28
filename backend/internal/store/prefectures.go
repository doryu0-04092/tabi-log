package store

import (
	"context"
	"fmt"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// PrefectureStore は都道府県マスタを読み出す。
//
// マスタは不変のため参照のみを提供する。
type PrefectureStore struct {
	q *dbgen.Queries
}

// NewPrefectureStore を作る。
//
// dbgen.DBTX を受け取るため、*sql.DB でも *sql.Tx でも渡せる。
// トランザクションの内側から使う場合も同じコードで済む。
func NewPrefectureStore(db dbgen.DBTX) *PrefectureStore {
	return &PrefectureStore{q: dbgen.New(db)}
}

// List は47件を JIS コード順で返す。
func (s *PrefectureStore) List(ctx context.Context) ([]domain.Prefecture, error) {
	rows, err := s.q.ListPrefectures(ctx)
	if err != nil {
		return nil, fmt.Errorf("都道府県マスタの取得に失敗した: %w", err)
	}

	// 生成された行の型をそのまま外へ出さない。列を1つ足しただけで
	// 上位層の見え方が変わるのを防ぐ。
	out := make([]domain.Prefecture, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Prefecture{
			Code:     r.Code,
			Name:     r.Name,
			NameKana: r.NameKana,
			Region:   r.Region,
		})
	}
	return out, nil
}
