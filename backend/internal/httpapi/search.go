package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/search"
)

// SearchRepository は検索の結果を返す。
//
// **投稿は ID の並びだけを返す。** 本体の組み立てはフィードと同じ手順を
// 通したいため、PostRepository 側の ListPostsByIDs に任せる。
type SearchRepository interface {
	SearchPosts(ctx context.Context, f search.Filters, cursor search.Cursor, limit int) ([]uint64, search.Cursor, error)
	SearchUsers(ctx context.Context, keyword string, cursorID uint64, limit int) ([]domain.User, uint64, error)
}

type searchHandler struct {
	repo    SearchRepository
	posts   PostRepository
	likes   ReactionRepository
	follows FollowRepository
	storage ObjectStorage
	logger  *slog.Logger
}

// ---------------------------------------------------------------------------
// 投稿を探す
// ---------------------------------------------------------------------------

func (h *searchHandler) SearchPosts(w http.ResponseWriter, r *http.Request, params gen.SearchPostsParams) {
	if _, ok := UserIDFrom(r.Context()); !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	filters, ok := h.buildFilters(w, r, params)
	if !ok {
		return
	}

	cursor, ok := h.parseSearchCursor(w, r, params.Cursor, filters.Sort)
	if !ok {
		return
	}

	writeFeedPage(w, r, h.likes, h.logger, params.Limit,
		func(ctx context.Context, limit int) ([]domain.Post, string, error) {
			ids, next, err := h.repo.SearchPosts(ctx, filters, cursor, limit)
			if err != nil {
				return nil, "", err
			}
			// **検索は並びだけを決め、本体はフィードと同じ手順で組み立てる。**
			// 画像・タグ・いいねの取り方を2か所に持つと必ず食い違う。
			posts, err := h.posts.ListPostsByIDs(ctx, ids, h.storage, displayURLTTL)
			if err != nil {
				return nil, "", err
			}
			return posts, formatSearchCursor(next, filters.Sort), nil
		})
}

// buildFilters は問い合わせ文字列を絞り込みの軸へ移す。
//
// 不正な指定は 400 を返し、黙って無視しない。無視すると
// 「絞り込んだつもりが全件出ている」ことに気づけない。
func (h *searchHandler) buildFilters(w http.ResponseWriter, r *http.Request, params gen.SearchPostsParams) (search.Filters, bool) {
	f := search.Filters{Sort: search.SortLatest}

	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		keyword := strings.TrimSpace(*params.Q)
		// **1文字では ngram の索引に当たらない。** 黙って空を返すと
		// 「壊れている」と受け取られるため、理由を伝えて弾く。
		if !search.ValidKeyword(keyword) {
			writeError(w, r, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("キーワードは%d文字以上で指定してください", search.MinKeywordRunes))
			return search.Filters{}, false
		}
		f.Keyword = keyword
	}
	if params.PrefectureCode != nil {
		f.PrefectureCode = *params.PrefectureCode
	}
	if params.Region != nil {
		f.Region = *params.Region
	}
	if params.Tag != nil {
		f.Tag = strings.TrimSpace(*params.Tag)
	}
	if params.Handle != nil {
		f.Handle = *params.Handle
	}
	if params.VisitedFrom != nil {
		t := params.VisitedFrom.Time
		f.VisitedFrom = &t
	}
	if params.VisitedTo != nil {
		t := params.VisitedTo.Time
		f.VisitedTo = &t
	}
	if params.Since != nil {
		t := params.Since.Time
		f.Since = &t
	}

	// 訪問日の範囲が逆さまなら、結果は必ず空になる。
	// 空の結果より、指定の誤りだと伝えるほうが親切である。
	if f.VisitedFrom != nil && f.VisitedTo != nil && f.VisitedTo.Before(*f.VisitedFrom) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "訪問日の範囲が逆になっています")
		return search.Filters{}, false
	}

	if params.Sort != nil && *params.Sort == gen.Popular {
		f.Sort = search.SortPopular
	}
	return f, true
}

// parseSearchCursor はカーソルを並び順に応じて読む。
//
// 新着は "<id>"、人気順は "<like_count>_<id>"。**形が違うため、
// 並び順を変えたまま前のカーソルを渡すと弾かれる。** 黙って先頭から
// 返すと、利用者から見て「同じ投稿がまた出てくる」ことになる。
func (h *searchHandler) parseSearchCursor(w http.ResponseWriter, r *http.Request, param *string, sort search.SortOrder) (search.Cursor, bool) {
	// 未指定なら「一番上から」を表す上限値にする。
	start := search.Cursor{LikeCount: ^uint32(0), ID: ^uint64(0)}
	if param == nil || *param == "" {
		return start, true
	}

	invalid := func() (search.Cursor, bool) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "カーソルの指定が不正です")
		return search.Cursor{}, false
	}

	if sort == search.SortPopular {
		before, after, found := strings.Cut(*param, "_")
		if !found {
			return invalid()
		}
		likes, err := strconv.ParseUint(before, 10, 32)
		if err != nil {
			return invalid()
		}
		id, err := strconv.ParseUint(after, 10, 64)
		if err != nil {
			return invalid()
		}
		return search.Cursor{LikeCount: uint32(likes), ID: id}, true
	}

	id, err := strconv.ParseUint(*param, 10, 64)
	if err != nil {
		return invalid()
	}
	return search.Cursor{ID: id}, true
}

// ---------------------------------------------------------------------------
// 利用者を探す
// ---------------------------------------------------------------------------

func (h *searchHandler) SearchUsers(w http.ResponseWriter, r *http.Request, params gen.SearchUsersParams) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	keyword := strings.TrimSpace(params.Q)
	if !search.ValidKeyword(keyword) {
		writeError(w, r, http.StatusBadRequest, "validation_error",
			fmt.Sprintf("検索する語は%d文字以上で指定してください", search.MinKeywordRunes))
		return
	}

	limit, ok := parseLimit(w, r, params.Limit, defaultUserLimit, maxUserLimit)
	if !ok {
		return
	}
	cursor, ok := parseCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	users, next, err := h.repo.SearchUsers(r.Context(), keyword, cursor, limit)
	if err != nil {
		feedError(w, r, h.logger, "利用者の検索に失敗した", err)
		return
	}

	// **一覧の1件ごとにフォローの状態を引かない。** まとめて1クエリで解決する。
	ids := make([]uint64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	followed, err := h.follows.FollowedUserIDs(r.Context(), viewerID, ids)
	if err != nil {
		feedError(w, r, h.logger, "フォローの状態を取得できない", err)
		return
	}

	items := make([]gen.UserSummary, 0, len(users))
	for _, u := range users {
		items = append(items, gen.UserSummary{
			Id:          int64(u.ID),
			Handle:      u.Handle,
			DisplayName: u.DisplayName,
			Bio:         u.Bio,
			IsFollowing: followed[u.ID],
			IsMe:        u.ID == viewerID,
		})
	}

	var body gen.UserListResponse
	body.Data.Users = items
	if next != 0 {
		s := strconv.FormatUint(next, 10)
		body.Data.NextCursor = &s
	}
	writeJSON(w, r, http.StatusOK, body.Data)
}

// ---------------------------------------------------------------------------
// カーソルの形
// ---------------------------------------------------------------------------

// searchCursorSeparator は人気順のカーソルの区切り。
const searchCursorSeparator = "_"

// formatSearchCursor は次のカーソルを並び順に応じた文字列にする。
//
// 新着は "<id>"、人気順は "<いいね数>_<id>"。**人気順は組でないと
// 「同じいいね数の中のどこまで返したか」を表せない。**
func formatSearchCursor(c search.Cursor, sort search.SortOrder) string {
	if c.ID == 0 {
		return ""
	}
	if sort == search.SortPopular {
		return strconv.FormatUint(uint64(c.LikeCount), 10) +
			searchCursorSeparator + strconv.FormatUint(c.ID, 10)
	}
	return strconv.FormatUint(c.ID, 10)
}
