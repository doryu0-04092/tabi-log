package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

const (
	maxCommentRunes     = 500
	defaultCommentLimit = 50
	maxCommentLimit     = 100
)

// ReactionRepository はいいねとコメントの永続化操作を表す。
type ReactionRepository interface {
	Like(ctx context.Context, userID, postID uint64) error
	Unlike(ctx context.Context, userID, postID uint64) error
	LikedPostIDs(ctx context.Context, userID uint64, postIDs []uint64) (map[uint64]bool, error)

	CreateComment(ctx context.Context, userID, postID uint64, body string) (uint64, error)
	GetComment(ctx context.Context, commentID uint64) (domain.Comment, error)
	DeleteComment(ctx context.Context, commentID, postID uint64) error
	FindCommentPermission(ctx context.Context, commentID uint64) (store.CommentPermission, error)
	ListComments(ctx context.Context, postID, cursorID uint64, limit int) ([]domain.Comment, uint64, error)
}

type reactionHandler struct {
	repo    ReactionRepository
	posts   PostRepository
	avatars *avatarResolver
	logger  *slog.Logger
	// createLimit はコメントの作成にかける上限。nil なら数えない。
	createLimit *writeLimiter
}

// ---------------------------------------------------------------------------
// いいね
// ---------------------------------------------------------------------------

func (h *reactionHandler) LikePost(w http.ResponseWriter, r *http.Request, postID gen.PostId) {
	h.toggleLike(w, r, uint64(postID), true)
}

func (h *reactionHandler) UnlikePost(w http.ResponseWriter, r *http.Request, postID gen.PostId) {
	h.toggleLike(w, r, uint64(postID), false)
}

// toggleLike はいいねの登録と取り消しをまとめる。
//
// どちらも「利用者を取り出す → 投稿の存在を確かめる → 冪等に実行する」で
// 手順が同じであり、分けて書くと片方だけ直す間違いが起きる。
func (h *reactionHandler) toggleLike(w http.ResponseWriter, r *http.Request, postID uint64, like bool) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	var err error
	if like {
		err = h.repo.Like(r.Context(), userID, postID)
	} else {
		err = h.repo.Unlike(r.Context(), userID, postID)
	}

	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "いいねの処理に失敗した", err)
		return
	}

	// **どちらも冪等なので、状態が変わらなくても 204 を返す。**
	// 「既にいいねしている」を失敗として返すと、連打や再送のたびに
	// 画面がエラーを出すことになる。
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// コメント
// ---------------------------------------------------------------------------

func (h *reactionHandler) CreateComment(w http.ResponseWriter, r *http.Request, postID gen.PostId) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	if !h.createLimit.allow(w, r, userID) {
		return
	}

	var req gen.CreateCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	body := strings.TrimSpace(req.Body)
	// 文字数で数える。日本語のコメントがバイト数で弾かれると
	// 「167文字で長すぎると言われる」ことになる。
	if n := utf8.RuneCountInString(body); n < 1 || n > maxCommentRunes {
		writeError(w, r, http.StatusBadRequest, "validation_error", "コメントは1〜500文字にしてください")
		return
	}

	commentID, err := h.repo.CreateComment(r.Context(), userID, uint64(postID), body)
	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "コメントの作成に失敗した", err)
		return
	}

	// 作成したものをそのまま返す。画面が再取得せずに描けるようにするため。
	// created_at はデータベースが付けるので、読み直さないと埋められない。
	comment, err := h.repo.GetComment(r.Context(), commentID)
	if err != nil {
		h.internalError(w, r, "作成したコメントを取得できない", err)
		return
	}

	// 投稿の所有者は渡さない。作成者は必ず自分であり、
	// canDelete はその時点で true に決まる。
	out := toAPIComment(comment, userID, userID)
	h.avatars.fillOne(r.Context(), &out.Author)
	writeJSON(w, r, http.StatusCreated, out)
}

func (h *reactionHandler) ListComments(w http.ResponseWriter, r *http.Request, postID gen.PostId, params gen.ListCommentsParams) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	limit := defaultCommentLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > maxCommentLimit {
		writeError(w, r, http.StatusBadRequest, "validation_error", "取得件数の指定が不正です")
		return
	}

	var cursor uint64
	if params.Cursor != nil && *params.Cursor != "" {
		v, err := strconv.ParseUint(*params.Cursor, 10, 64)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "カーソルの指定が不正です")
			return
		}
		cursor = v
	}

	// 投稿の所有者は自分の投稿のコメントを全て消せるため、
	// 削除の可否を判定するのに必要になる。
	owner, err := h.posts.PostOwner(r.Context(), uint64(postID))
	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "投稿の所有者を取得できない", err)
		return
	}

	comments, next, err := h.repo.ListComments(r.Context(), uint64(postID), cursor, limit)
	if err != nil {
		h.internalError(w, r, "コメントの取得に失敗した", err)
		return
	}

	items := make([]gen.Comment, 0, len(comments))
	for _, c := range comments {
		items = append(items, toAPIComment(c, userID, owner))
	}

	authors := make([]*gen.User, 0, len(items))
	for i := range items {
		authors = append(authors, &items[i].Author)
	}
	h.avatars.fill(r.Context(), authors)

	var body gen.CommentListResponse
	body.Data.Comments = items
	if next != 0 {
		s := strconv.FormatUint(next, 10)
		body.Data.NextCursor = &s
	}
	writeJSON(w, r, http.StatusOK, body.Data)
}

func (h *reactionHandler) DeleteComment(w http.ResponseWriter, r *http.Request, commentID int64) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	perm, err := h.repo.FindCommentPermission(r.Context(), uint64(commentID))
	if errors.Is(err, store.ErrCommentNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "コメントが見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "コメントの権限を確認できない", err)
		return
	}

	// **投稿の所有者にも削除させる。**
	// 自分の投稿に付いた不快なコメントを消せないと、
	// 「投稿ごと消す」以外の手段が無くなる。
	if !canDeleteComment(perm, userID) {
		writeError(w, r, http.StatusForbidden, "forbidden", "このコメントを削除する権限がありません")
		return
	}

	if err := h.repo.DeleteComment(r.Context(), perm.CommentID, perm.PostID); err != nil {
		if errors.Is(err, store.ErrCommentNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "コメントが見つかりません")
			return
		}
		h.internalError(w, r, "コメントの削除に失敗した", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// canDeleteComment はコメントを削除できるかを判定する。
//
// 規則をここ1か所に置いているのは、判定と表示（canDelete）で
// 同じ規則を使うためである。分かれていると「ボタンは出るが消せない」
// あるいはその逆が起きる。
func canDeleteComment(perm store.CommentPermission, userID uint64) bool {
	return perm.CommentUser == userID || perm.PostOwner == userID
}

func (h *reactionHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	respondInternalError(w, r, h.logger, msg, err)
}

func toAPIComment(c domain.Comment, viewerID, postOwnerID uint64) gen.Comment {
	return gen.Comment{
		Id:        int64(c.ID),
		Author:    toAPIUser(c.Author),
		Body:      c.Body,
		CreatedAt: c.CreatedAt,
		CanDelete: c.Author.ID == viewerID || postOwnerID == viewerID,
	}
}
