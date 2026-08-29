package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

// maxCursorID はカーソル未指定のときの起点。
//
// 「一番新しいものから」を「id が上限より小さいもの」として表す。
// 先頭ページ専用のクエリを別に持つと、同じ SELECT を2つ保守することになる。
const maxCursorID = ^uint64(0)

// ListFeed は新着フィードを返す。
//
// nextCursor が 0 なら、それ以上の投稿は無い。
func (s *PostStore) ListFeed(
	ctx context.Context,
	cursorID uint64,
	limit int,
	signer storage.URLSigner,
	urlTTL time.Duration,
) ([]domain.Post, uint64, error) {
	if cursorID == 0 {
		cursorID = maxCursorID
	}

	// **1件多く取る。** 返す件数と同じだけ取ると「続きがあるか」が分からず、
	// 次のページを取るまで判定できない。1件多く取って、あれば捨てる。
	rows, err := s.q.ListPostsBefore(ctx, dbgen.ListPostsBeforeParams{
		ID:    cursorID,
		Limit: int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("フィードの取得に失敗した: %w", err)
	}
	return s.buildPage(ctx, rows, limit, signer, urlTTL)
}

// ListFollowingFeed はフォロー中の利用者の投稿を新しい順に返す。
//
// **自分の投稿は含まない。** 自分自身はフォローできないため、
// 「フォローしている人の投稿」に自分は入らない。
func (s *PostStore) ListFollowingFeed(
	ctx context.Context,
	viewerID uint64,
	cursorID uint64,
	limit int,
	signer storage.URLSigner,
	urlTTL time.Duration,
) ([]domain.Post, uint64, error) {
	if cursorID == 0 {
		cursorID = maxCursorID
	}

	rows, err := s.q.ListFollowingFeedBefore(ctx, dbgen.ListFollowingFeedBeforeParams{
		FollowerID: viewerID,
		ID:         cursorID,
		Limit:      int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("フォロー中フィードの取得に失敗した: %w", err)
	}

	converted := make([]dbgen.ListPostsBeforeRow, 0, len(rows))
	for _, r := range rows {
		converted = append(converted, dbgen.ListPostsBeforeRow(r))
	}
	return s.buildPage(ctx, converted, limit, signer, urlTTL)
}

// ListUserPosts はある利用者の投稿を新しい順に返す。
//
// 並びとカーソルの扱いは新着フィードと同じである。違うのは絞り込みだけなので、
// 組み立ては buildPage に寄せてある。
func (s *PostStore) ListUserPosts(
	ctx context.Context,
	userID uint64,
	cursorID uint64,
	limit int,
	signer storage.URLSigner,
	urlTTL time.Duration,
) ([]domain.Post, uint64, error) {
	if cursorID == 0 {
		cursorID = maxCursorID
	}

	rows, err := s.q.ListPostsByUserBefore(ctx, dbgen.ListPostsByUserBeforeParams{
		UserID: userID,
		ID:     cursorID,
		Limit:  int32(limit + 1),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("投稿の取得に失敗した: %w", err)
	}

	// 選ぶ列が同じなので、生成された2つの行の型は項目も並びも一致する。
	// 変換できなくなったら、それは SELECT がずれた合図である。
	converted := make([]dbgen.ListPostsBeforeRow, 0, len(rows))
	for _, r := range rows {
		converted = append(converted, dbgen.ListPostsBeforeRow(r))
	}
	return s.buildPage(ctx, converted, limit, signer, urlTTL)
}

// buildPage は取得した行を1ページ分の投稿に組み立てる。
//
// rows は「限度より1件多く取ったもの」を受け取り、続きの有無をここで判定する。
func (s *PostStore) buildPage(
	ctx context.Context,
	rows []dbgen.ListPostsBeforeRow,
	limit int,
	signer storage.URLSigner,
	urlTTL time.Duration,
) (posts []domain.Post, nextCursor uint64, err error) {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	posts, err = s.assemble(ctx, rows, signer, urlTTL)
	if err != nil {
		return nil, 0, err
	}

	if hasMore && len(rows) > 0 {
		nextCursor = rows[len(rows)-1].ID
	}
	return posts, nextCursor, nil
}

// ListPostsByIDs は指定した ID の投稿を、**渡された並びのまま**返す。
//
// 検索は「どの投稿がどの順で並ぶか」だけを決め、本体の組み立ては
// フィードと同じ手順を通す。並べ直しをここでやるのは、
// IN 句の結果がデータベース側の都合の順で返るためである。
func (s *PostStore) ListPostsByIDs(
	ctx context.Context,
	ids []uint64,
	signer storage.URLSigner,
	urlTTL time.Duration,
) ([]domain.Post, error) {
	if len(ids) == 0 {
		return []domain.Post{}, nil
	}

	rows, err := s.q.ListPostsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("投稿の取得に失敗した: %w", err)
	}

	converted := make([]dbgen.ListPostsBeforeRow, 0, len(rows))
	for _, r := range rows {
		converted = append(converted, dbgen.ListPostsBeforeRow(r))
	}
	posts, err := s.assemble(ctx, converted, signer, urlTTL)
	if err != nil {
		return nil, err
	}

	byID := make(map[uint64]domain.Post, len(posts))
	for _, p := range posts {
		byID[p.ID] = p
	}
	ordered := make([]domain.Post, 0, len(ids))
	for _, id := range ids {
		// 検索した直後に消された投稿は落ちる。
		// 見つからないものを飛ばすだけでよく、失敗にはしない。
		if p, ok := byID[id]; ok {
			ordered = append(ordered, p)
		}
	}
	return ordered, nil
}

// assemble は取得した行を投稿に組み立てる。
//
// 画像・変換物・タグは**それぞれ1クエリでまとめて取る。**
// 投稿ごとに引くと20件で20往復になる（N+1）。
func (s *PostStore) assemble(
	ctx context.Context,
	rows []dbgen.ListPostsBeforeRow,
	signer storage.URLSigner,
	urlTTL time.Duration,
) ([]domain.Post, error) {
	if len(rows) == 0 {
		return []domain.Post{}, nil
	}

	postIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		postIDs = append(postIDs, r.ID)
	}

	mediaByPost, err := s.mediaByPost(ctx, postIDs, signer, urlTTL)
	if err != nil {
		return nil, err
	}
	tagsByPost, err := s.tagsByPost(ctx, postIDs)
	if err != nil {
		return nil, err
	}

	posts := make([]domain.Post, 0, len(rows))
	for _, r := range rows {
		posts = append(posts, domain.Post{
			ID: r.ID,
			Author: domain.User{
				ID:          r.UserID,
				Handle:      r.Handle,
				DisplayName: r.DisplayName,
				Bio:         nullStringToPtr(r.Bio),
			},
			Body: r.Body,
			Prefecture: domain.Prefecture{
				Code:     r.PrefectureCode,
				Name:     r.PrefectureName,
				NameKana: r.PrefectureNameKana,
				Region:   r.Region,
			},
			SpotName:     nullStringToPtr(r.SpotName),
			VisitedOn:    r.VisitedOn,
			Media:        mediaByPost[r.ID],
			Tags:         tagsByPost[r.ID],
			LikeCount:    int(r.LikeCount),
			CommentCount: int(r.CommentCount),
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return posts, nil
}

// mediaByPost は複数の投稿の画像を投稿IDごとにまとめて返す。
func (s *PostStore) mediaByPost(
	ctx context.Context,
	postIDs []uint64,
	signer storage.URLSigner,
	urlTTL time.Duration,
) (map[uint64][]domain.PostMedia, error) {
	ids := make([]sql.NullInt64, 0, len(postIDs))
	for _, id := range postIDs {
		ids = append(ids, sql.NullInt64{Int64: int64(id), Valid: true})
	}

	mediaRows, err := s.q.ListMediaByPostIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("画像の取得に失敗した: %w", err)
	}
	variantRows, err := s.q.ListVariantsByPostIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("変換物の取得に失敗した: %w", err)
	}

	keysByMedia := make(map[uint64]map[string]string, len(mediaRows))
	for _, v := range variantRows {
		if keysByMedia[v.MediaID] == nil {
			keysByMedia[v.MediaID] = map[string]string{}
		}
		keysByMedia[v.MediaID][string(v.Kind)] = v.S3Key
	}

	out := make(map[uint64][]domain.PostMedia, len(postIDs))
	for _, m := range mediaRows {
		thumb, medium, err := signVariants(ctx, signer, keysByMedia[m.ID], urlTTL)
		if err != nil {
			return nil, err
		}
		postID := uint64(m.PostID.Int64)
		out[postID] = append(out[postID], domain.PostMedia{
			ID:        m.ID,
			AltText:   m.AltText.String,
			Width:     int(m.Width.Int32),
			Height:    int(m.Height.Int32),
			ThumbURL:  thumb,
			MediumURL: medium,
		})
	}
	return out, nil
}

// tagsByPost は複数の投稿のタグを投稿IDごとにまとめて返す。
func (s *PostStore) tagsByPost(ctx context.Context, postIDs []uint64) (map[uint64][]string, error) {
	rows, err := s.q.ListTagsByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, fmt.Errorf("タグの取得に失敗した: %w", err)
	}
	out := make(map[uint64][]string, len(postIDs))
	for _, r := range rows {
		out[r.PostID] = append(out[r.PostID], r.Name)
	}
	return out, nil
}
