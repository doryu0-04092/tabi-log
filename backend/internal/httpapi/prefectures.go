package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

// PrefectureLister は都道府県マスタの読み出しを表す。
//
// store の具体型ではなくインターフェースを受けるのは、
// ハンドラのテストにデータベースを要らなくするためである。
// インターフェースは使う側（この層）で宣言する。
type PrefectureLister interface {
	List(ctx context.Context) ([]domain.Prefecture, error)
}

type prefectureHandler struct {
	store  PrefectureLister
	logger *slog.Logger
}

// ListPrefectures は都道府県マスタ47件を JIS コード順で返す。
func (h *prefectureHandler) ListPrefectures(w http.ResponseWriter, r *http.Request) {
	prefectures, err := h.store.List(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "都道府県マスタの取得に失敗した",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
		return
	}

	// 内容が不変なので、ブラウザに長く保持させてよい。
	//
	// public を付けても安全である。利用者ごとに内容が変わらず、
	// 認証も要さない公開情報のためである（認証済みの応答に public を付けると
	// 共有キャッシュ経由で他人に配られる事故になる）。
	w.Header().Set("Cache-Control", "public, max-age=86400")

	writeJSON(w, r, http.StatusOK, toAPIPrefectures(prefectures))
}

// toAPIPrefectures はドメインの型を仕様から生成した型へ変換する。
//
// 変換を明示的に書くのは、ドメインに列や項目が増えたときに
// API の出力が黙って変わらないようにするためである。
func toAPIPrefectures(src []domain.Prefecture) []gen.Prefecture {
	out := make([]gen.Prefecture, 0, len(src))
	for _, p := range src {
		out = append(out, toAPIPrefecture(p))
	}
	return out
}

func toAPIPrefecture(p domain.Prefecture) gen.Prefecture {
	return gen.Prefecture{
		Code:     p.Code,
		Name:     p.Name,
		NameKana: p.NameKana,
		Region:   gen.PrefectureRegion(p.Region),
	}
}
