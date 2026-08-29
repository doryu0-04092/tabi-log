package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

// 投稿の一覧は「新着」「フォロー中」「ある利用者の投稿」の3つがあり、
// 違うのは**どの投稿を取ってくるか**だけである。件数とカーソルの検証、
// いいねの状態の一括取得、応答の組み立ては共通なのでここに置く。
//
// 3か所に写しを置くと、たとえば「いいねの状態をまとめて引く」を1か所だけ
// 直し忘れる、といった食い違いが起きる。

// parseLimit は件数の指定を検証する。範囲外なら応答を書いて false を返す。
func parseLimit(w http.ResponseWriter, r *http.Request, param *int, def, max int) (int, bool) {
	limit := def
	if param != nil {
		limit = *param
	}
	if limit < 1 || limit > max {
		writeError(w, r, http.StatusBadRequest, "validation_error", "取得件数の指定が不正です")
		return 0, false
	}
	return limit, true
}

// parseCursor はカーソルの指定を検証する。省略時は 0（先頭から）。
//
// カーソルは前回の応答をそのまま渡す値である。解釈できないものは受け付けず、
// 先頭から返してごまかさない。
func parseCursor(w http.ResponseWriter, r *http.Request, param *string) (uint64, bool) {
	if param == nil || *param == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(*param, 10, 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "カーソルの指定が不正です")
		return 0, false
	}
	return v, true
}

// fetchPage は投稿を1ページ分取ってくる。取り方の違いだけをここで受け取る。
//
// **次のカーソルは文字列で返す。** 新着とフォロー中は投稿 ID だが、
// 人気順は (いいね数, ID) の組であり数値1つに収まらない。
// 続きが無いときは空文字を返す。
type fetchPage func(ctx context.Context, limit int) ([]domain.Post, string, error)

// formatCursor は ID をカーソル文字列にする。0 は「続きが無い」を表す。
func formatCursor(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// writeFeedPage は投稿の一覧を検証・取得・組み立てまで通して応答する。
func writeFeedPage(
	w http.ResponseWriter,
	r *http.Request,
	likes ReactionRepository,
	logger *slog.Logger,
	limitParam *int,
	fetch fetchPage,
) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	limit, ok := parseLimit(w, r, limitParam, defaultFeedLimit, maxFeedLimit)
	if !ok {
		return
	}

	posts, next, err := fetch(r.Context(), limit)
	if err != nil {
		feedError(w, r, logger, "フィードの取得に失敗した", err)
		return
	}

	// **いいねの状態は20件分を1クエリでまとめて引く。**
	// 投稿ごとに問い合わせると20回の往復になる（N+1）。
	postIDs := make([]uint64, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
	}
	liked, err := likes.LikedPostIDs(r.Context(), viewerID, postIDs)
	if err != nil {
		feedError(w, r, logger, "いいねの状態を取得できない", err)
		return
	}

	items := make([]gen.Post, 0, len(posts))
	for _, p := range posts {
		items = append(items, toAPIPost(p, viewerID, liked[p.ID]))
	}

	var body gen.PostListResponse
	body.Data.Posts = items
	if next != "" {
		body.Data.NextCursor = &next
	}
	writeJSON(w, r, http.StatusOK, body.Data)
}

func feedError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, msg string, err error) {
	attrs := []any{slog.String("request_id", RequestIDFrom(r.Context()))}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	logger.ErrorContext(r.Context(), msg, attrs...)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}
