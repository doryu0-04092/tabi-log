package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
)

// AvatarRepository はアバターの設定と取得を表す。
type AvatarRepository interface {
	SetAvatar(ctx context.Context, userID, mediaID uint64) error
	ClearAvatar(ctx context.Context, userID uint64) error
	AvatarKeys(ctx context.Context, userIDs []uint64) (map[uint64]string, error)
}

// avatarResolver は応答に載せる利用者へ、閲覧者ごとに変わる情報を埋める。
//
// 埋めるのはアバターのURLと**フォローの状態**である。どちらも
// 「誰が見ているか」で変わり、投稿・コメント・通知・一覧のあらゆる応答に
// 出てくる。**埋める場所ごとに引き方を書くと、1か所だけ「1件ずつ引く」
// 実装が紛れ込んでも気づけない。** まとめて引く手順をここ1つに置く。
type avatarResolver struct {
	repo    AvatarRepository
	follows FollowRepository
	storage ObjectStorage
	logger  *slog.Logger
}

// fill は渡した利用者すべてにアバターとフォローの状態を埋める。
//
// **1件ずつ引かない。** フィード20件に対して20回の往復になる。
// 署名は鍵の数だけ行うが、これは通信を伴わない計算である。
//
// 失敗しても応答は返す。アバターが出ない・フォローの状態が分からない
// だけで、投稿は読める。付随する情報のために一覧全体を落とすのは
// 割に合わない。
func (a *avatarResolver) fill(ctx context.Context, users []*gen.User) {
	if a == nil || len(users) == 0 {
		return
	}

	ids := make([]uint64, 0, len(users))
	seen := make(map[uint64]struct{}, len(users))
	for _, u := range users {
		id := uint64(u.Id)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	urls := a.urls(ctx, ids)

	// **フォローの状態も閲覧者ごとに変わる。** 未認証（あり得ないが）や
	// 取得に失敗した場合は false のままにし、導線を出さない側へ倒す。
	viewerID, _ := UserIDFrom(ctx)
	followed := map[uint64]bool{}
	if viewerID != 0 && a.follows != nil {
		got, err := a.follows.FollowedUserIDs(ctx, viewerID, ids)
		if err != nil {
			a.logger.ErrorContext(ctx, "フォローの状態を取得できない", slog.String("error", err.Error()))
		} else {
			followed = got
		}
	}

	for _, u := range users {
		id := uint64(u.Id)
		if url, ok := urls[id]; ok {
			v := url
			u.AvatarUrl = &v
		}
		isFollowing := followed[id]
		isMe := id == viewerID
		u.IsFollowing = &isFollowing
		u.IsMe = &isMe
	}
}

// fillOne は1人ぶんを埋める。
func (a *avatarResolver) fillOne(ctx context.Context, u *gen.User) {
	a.fill(ctx, []*gen.User{u})
}

// urls は利用者IDごとのアバターURLを返す。
//
// gen.User と gen.UserSummary / gen.UserProfile は生成された別々の型であり、
// 共通の項目を持たない。**埋める側で使えるよう、解決だけを切り出す。**
func (a *avatarResolver) urls(ctx context.Context, userIDs []uint64) map[uint64]string {
	if a == nil || len(userIDs) == 0 {
		return map[uint64]string{}
	}

	keys, err := a.repo.AvatarKeys(ctx, userIDs)
	if err != nil {
		a.logger.ErrorContext(ctx, "アバターの鍵を取得できない", slog.String("error", err.Error()))
		return map[uint64]string{}
	}

	out := make(map[uint64]string, len(keys))
	for id, key := range keys {
		url, err := a.storage.PresignGet(ctx, key, displayURLTTL)
		if err != nil {
			a.logger.ErrorContext(ctx, "アバターのURLを作れない",
				slog.Uint64("user_id", id), slog.String("error", err.Error()))
			continue
		}
		out[id] = url
	}
	return out
}

// ---------------------------------------------------------------------------
// アバターの設定・解除
// ---------------------------------------------------------------------------

func (h *accountHandler) SetAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	var req gen.SetAvatarRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.MediaId < 1 {
		writeError(w, r, http.StatusBadRequest, "validation_error", "画像の指定が不正です")
		return
	}

	if err := h.repo.SetAvatar(r.Context(), userID, uint64(req.MediaId)); err != nil {
		// **理由は区別しない。** 他人の画像 / 未処理 / 使用済み を分けて返すと、
		// ID を総当たりして他人の画像の存在を調べられる。
		writeError(w, r, http.StatusBadRequest, "validation_error", "この画像はアバターに使えません")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *accountHandler) ClearAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	if err := h.repo.ClearAvatar(r.Context(), userID); err != nil {
		h.internalError(w, r, "アバターの解除に失敗した", err)
		return
	}

	// 設定していなくても 204。連打や再送で画面がエラーを出さないようにする。
	w.WriteHeader(http.StatusNoContent)
}
