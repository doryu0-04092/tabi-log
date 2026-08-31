package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"golang.org/x/text/unicode/norm"
)

// originalsPrefix はアップロードされた原本を置く接頭辞。
//
// **S3 のライフサイクルルールはこの接頭辞に対して設定する。**
// 投稿が確定されずに残ったオブジェクトを期限で削除するためである。
const originalsPrefix = "originals/"

// presignTTL は署名付きURLの有効期間。
//
// 短くしすぎると、回線が細い環境で大きな画像を送っている途中に失効する。
const presignTTL = 15 * time.Minute

// displayURLTTL は表示用URLの有効期間。
const displayURLTTL = 1 * time.Hour

// contentTypeExtensions は Content-Type から拡張子を決める。
//
// **クライアントが送ってきたファイル名は使わない。** 名前は利用者が自由に
// 決められ、パス区切りや上位ディレクトリを含めることもできる。
// こちらで決めた値だけをキーに使う。
var contentTypeExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// PostRepository は投稿と画像の永続化操作を表す。
type PostRepository interface {
	CreatePendingMedia(ctx context.Context, userID uint64, s3Key string) (uint64, error)
	CreatePost(ctx context.Context, in store.CreatePostInput) (uint64, error)
	UpdatePost(ctx context.Context, in store.UpdatePostInput) error
	DeletePost(ctx context.Context, postID, userID uint64) ([]string, error)
	PostOwner(ctx context.Context, postID uint64) (uint64, error)
	FindMediaByID(ctx context.Context, mediaID uint64) (store.MediaRecord, error)

	// ListOriginalKeys は投稿に紐づいた原本のキーを返す。
	// **保持印を付ける対象を知るために使う。**
	ListOriginalKeys(ctx context.Context, postID uint64) ([]string, error)
	GetPost(ctx context.Context, postID uint64, signer storage.URLSigner, ttl time.Duration) (domain.Post, error)
	ListFeed(ctx context.Context, cursorID uint64, limit int, signer storage.URLSigner, ttl time.Duration) ([]domain.Post, uint64, error)
	ListUserPosts(ctx context.Context, userID, cursorID uint64, limit int, signer storage.URLSigner, ttl time.Duration) ([]domain.Post, uint64, error)
	ListFollowingFeed(ctx context.Context, viewerID, cursorID uint64, limit int, signer storage.URLSigner, ttl time.Duration) ([]domain.Post, uint64, error)
	ListPostsByIDs(ctx context.Context, ids []uint64, signer storage.URLSigner, ttl time.Duration) ([]domain.Post, error)
	ListUserTravels(ctx context.Context, userID uint64, cursor store.TravelCursor, limit int, signer storage.URLSigner, ttl time.Duration) ([]domain.Post, store.TravelCursor, error)
}

// ObjectStorage は画像の保存先。
type ObjectStorage interface {
	storage.URLSigner
	PresignPut(ctx context.Context, key, contentType string, contentLength int64, ttl time.Duration) (string, error)

	// MarkKept は投稿に使われた原本を期限削除の対象から外す。
	// **これを呼ばないと、原本が7日で消えて別解像度を作れなくなる。**
	MarkKept(ctx context.Context, keys ...string) error

	Delete(ctx context.Context, keys ...string) error
}

type postHandler struct {
	repo    PostRepository
	storage ObjectStorage
	// likes は「自分がいいねしているか」を解決するために使う。
	// フィードでは20件分をまとめて引く（投稿ごとに問い合わせない）。
	likes   ReactionRepository
	avatars *avatarResolver
	logger  *slog.Logger
	now     func() time.Time
	// createLimit は投稿の作成にかける上限。nil なら数えない。
	createLimit *writeLimiter

	// uploadLimit は署名付き URL の発行回数。**投稿の上限では止まらない。**
	// 発行するだけで S3 の PUT・Lambda の起動・行の追加が起きる。
	uploadLimit *writeLimiter

	// deletions は消し損ねた S3 のオブジェクトの控え先。
	// **控えないと、行が消えたあとの削除失敗を誰も拾えない。**
	deletions DeletionQueue
}

// ---------------------------------------------------------------------------
// 署名付きURLの発行
// ---------------------------------------------------------------------------

func (h *postHandler) PresignMediaUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	// **投稿の上限では止まらない経路である。** 発行1回ごとに
	// S3 の PUT・Lambda の起動・行の追加が起きるため、投稿を
	// 1件も作らずに資源を消費し続けられる。
	//
	// 検証より先に数える。**通らないリクエストでも回数を使わせる。**
	// 後ろに置くと、わざと不正な値を送ることで無制限に試せる。
	if !h.uploadLimit.allow(w, r, userID) {
		return
	}

	var req gen.PresignRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	ext, ok := contentTypeExtensions[string(req.ContentType)]
	if !ok {
		writeError(w, r, http.StatusBadRequest, "validation_error", "対応していない画像形式です")
		return
	}
	if req.ContentLength <= 0 || req.ContentLength > maxUploadBytes {
		writeError(w, r, http.StatusBadRequest, "validation_error", "画像のサイズが上限を超えています")
		return
	}

	key, err := newObjectKey(ext)
	if err != nil {
		h.internalError(w, r, "オブジェクトキーの生成に失敗した", err)
		return
	}

	// **署名付きURLを発行する前に記録する（write-ahead）。**
	// 発行してから記録すると、その間にプロセスが止まった場合に
	// 「誰も知らないURLだけが有効」という状態が残る。
	mediaID, err := h.repo.CreatePendingMedia(r.Context(), userID, key)
	if err != nil {
		h.internalError(w, r, "画像の記録に失敗した", err)
		return
	}

	url, err := h.storage.PresignPut(r.Context(), key, string(req.ContentType), req.ContentLength, presignTTL)
	if err != nil {
		h.internalError(w, r, "アップロード用URLの発行に失敗した", err)
		return
	}

	var body gen.PresignResponse
	body.Data.MediaId = int64(mediaID)
	body.Data.UploadUrl = url
	body.Data.ExpiresIn = int(presignTTL.Seconds())
	writeJSON(w, r, http.StatusCreated, body.Data)
}

// maxUploadBytes は受け付ける画像の最大バイト数。仕様の maximum と揃える。
const maxUploadBytes = 10 * 1024 * 1024

// newObjectKey は推測しにくいオブジェクトキーを作る。
//
// 連番にすると、キーを1つ知るだけで他人の画像のキーを推測できる。
// 表示用URLは署名付きだが、キーの推測可能性を残す理由がない。
func newObjectKey(ext string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return originalsPrefix + hex.EncodeToString(b) + ext, nil
}

// ---------------------------------------------------------------------------
// 投稿の作成
// ---------------------------------------------------------------------------

func (h *postHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	// **本文を読む前に数える。** 上限を超えている相手の本文を読む必要は無い。
	if !h.createLimit.allow(w, r, userID) {
		return
	}

	var req gen.CreatePostRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	spot := trimOptional(req.SpotName)
	tags, msg := normalizeTags(req.Tags)
	if msg == "" {
		msg = validatePostFields(req.Body, req.PrefectureCode, spot, optionalDate(req.VisitedOn), h.now())
	}
	if msg == "" {
		msg = validateMediaInput(req.Media)
	}
	if msg != "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", msg)
		return
	}

	media := make([]store.MediaAttachment, 0, len(req.Media))
	for _, m := range req.Media {
		media = append(media, store.MediaAttachment{MediaID: uint64(m.MediaId)})
	}

	postID, err := h.repo.CreatePost(r.Context(), store.CreatePostInput{
		UserID:         userID,
		Body:           strings.TrimSpace(req.Body),
		PrefectureCode: req.PrefectureCode,
		SpotName:       spot,
		VisitedOn:      optionalDate(req.VisitedOn),
		Tags:           tags,
		Media:          media,
	})
	switch {
	case errors.Is(err, store.ErrMediaNotUsable):
		writeError(w, r, http.StatusBadRequest, "media_not_usable",
			"指定された画像は使えません。アップロードが完了しているか確認してください")
		return
	case err != nil:
		h.internalError(w, r, "投稿の作成に失敗した", err)
		return
	}

	h.markOriginalsKept(r, postID)

	h.respondWithPost(w, r, postID, http.StatusCreated)
}

// markOriginalsKept は投稿に使われた原本を、期限削除の対象から外す。
//
// **投稿の作成そのものは失敗させない。** 印が付かなくても投稿は成立しており、
// 利用者から見て壊れてはいない。ここで 500 を返すと、
// 「投稿はできているのにエラーが出る」という最も分かりにくい形になる。
//
// **ただし黙って落とさない。** 印が付かないと原本が期限で消え、
// 別解像度を後から作れなくなる。ERROR で残して気づけるようにする。
func (h *postHandler) markOriginalsKept(r *http.Request, postID uint64) {
	keys, err := h.repo.ListOriginalKeys(r.Context(), postID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "原本の保持印を付ける対象を引けなかった",
			"post_id", postID, "error", err)
		return
	}
	if len(keys) == 0 {
		return
	}
	if err := h.storage.MarkKept(r.Context(), keys...); err != nil {
		h.logger.ErrorContext(r.Context(), "原本の保持印を付けられなかった",
			"post_id", postID, "keys", strings.Join(keys, ","), "error", err)
	}
}

// ---------------------------------------------------------------------------
// 投稿の取得
// ---------------------------------------------------------------------------

func (h *postHandler) GetPost(w http.ResponseWriter, r *http.Request, postID gen.PostId) {
	h.respondWithPost(w, r, uint64(postID), http.StatusOK)
}

// ---------------------------------------------------------------------------
// 投稿の編集
// ---------------------------------------------------------------------------

func (h *postHandler) UpdatePost(w http.ResponseWriter, r *http.Request, postID gen.PostId) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}
	if !h.requireOwner(w, r, uint64(postID), userID) {
		return
	}

	var req gen.UpdatePostRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	spot := trimOptional(req.SpotName)
	tags, msg := normalizeTags(req.Tags)
	if msg == "" {
		msg = validatePostFields(req.Body, req.PrefectureCode, spot, optionalDate(req.VisitedOn), h.now())
	}
	if msg != "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", msg)
		return
	}

	if err := h.repo.UpdatePost(r.Context(), store.UpdatePostInput{
		PostID:         uint64(postID),
		UserID:         userID,
		Body:           strings.TrimSpace(req.Body),
		PrefectureCode: req.PrefectureCode,
		SpotName:       spot,
		VisitedOn:      optionalDate(req.VisitedOn),
		Tags:           tags,
	}); err != nil {
		h.internalError(w, r, "投稿の更新に失敗した", err)
		return
	}

	h.respondWithPost(w, r, uint64(postID), http.StatusOK)
}

// ---------------------------------------------------------------------------
// 投稿の削除
// ---------------------------------------------------------------------------

func (h *postHandler) DeletePost(w http.ResponseWriter, r *http.Request, postID gen.PostId) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}
	if !h.requireOwner(w, r, uint64(postID), userID) {
		return
	}

	keys, err := h.repo.DeletePost(r.Context(), uint64(postID), userID)
	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "投稿の削除に失敗した", err)
		return
	}

	// S3 の削除が失敗しても、投稿の削除自体は成功として返す。
	//
	// データベース上は既に消えており、利用者から見て投稿は消えている。
	// ここで 500 を返すと「削除できていない」と誤解させる。
	//
	// **消せなかったものは控える。** ライフサイクルは拾ってくれない。
	// 原本には state=kept が付いていて対象外、変換物には
	// ライフサイクル自体が無い（消すと表示中の投稿が壊れる）。
	deleteObjects(r, h.storage, h.deletions, h.logger,
		"投稿は削除したがS3のオブジェクトを削除できなかった", keys)

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// 共通処理
// ---------------------------------------------------------------------------

// requireOwner は投稿の所有者であることを確認する。
//
// **権限確認はサーバー側で行う。** 画面側で編集ボタンを隠すのは操作性のためで、
// 権限の根拠にはならない。API を直接叩けば誰でも試せる。
func (h *postHandler) requireOwner(w http.ResponseWriter, r *http.Request, postID, userID uint64) bool {
	owner, err := h.repo.PostOwner(r.Context(), postID)
	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return false
	}
	if err != nil {
		h.internalError(w, r, "投稿の所有者を確認できない", err)
		return false
	}
	if owner != userID {
		writeError(w, r, http.StatusForbidden, "forbidden", "この投稿を操作する権限がありません")
		return false
	}
	return true
}

func (h *postHandler) respondWithPost(w http.ResponseWriter, r *http.Request, postID uint64, status int) {
	viewerID, _ := UserIDFrom(r.Context())

	post, err := h.repo.GetPost(r.Context(), postID, h.storage, displayURLTTL)
	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "投稿の取得に失敗した", err)
		return
	}

	liked, err := h.likes.LikedPostIDs(r.Context(), viewerID, []uint64{postID})
	if err != nil {
		h.internalError(w, r, "いいねの状態を取得できない", err)
		return
	}

	out := toAPIPost(post, viewerID, liked[postID])
	h.avatars.fillOne(r.Context(), &out.Author)
	writeJSON(w, r, status, out)
}

func (h *postHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	respondInternalError(w, r, h.logger, msg, err)
}

// toAPIPost はドメインの型を API の型へ変換する。
//
// isLiked と canDelete は**閲覧者によって変わる**ため、
// 投稿そのものではなく引数として受け取る。
func toAPIPost(p domain.Post, viewerID uint64, isLiked bool) gen.Post {
	media := make([]gen.Media, 0, len(p.Media))
	for _, m := range p.Media {
		media = append(media, gen.Media{
			Id:        int64(m.ID),
			Width:     m.Width,
			Height:    m.Height,
			ThumbUrl:  m.ThumbURL,
			MediumUrl: m.MediumURL,
		})
	}

	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}

	return gen.Post{
		Id:           int64(p.ID),
		Author:       toAPIUser(p.Author),
		Body:         p.Body,
		Prefecture:   toAPIPrefecture(p.Prefecture),
		SpotName:     p.SpotName,
		VisitedOn:    optionalOpenapiDate(p.VisitedOn),
		Media:        media,
		Tags:         tags,
		LikeCount:    p.LikeCount,
		CommentCount: p.CommentCount,
		IsLiked:      isLiked,
		CanDelete:    p.Author.ID == viewerID,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    &p.UpdatedAt,
	}
}

// ---------------------------------------------------------------------------
// 入力の検証
// ---------------------------------------------------------------------------

const (
	maxBodyRunes     = 1000
	maxSpotNameRunes = 100
	maxAltTextRunes  = 200
	maxTags          = 5
	maxTagRunes      = 50
	maxMediaPerPost  = 4
)

func validatePostFields(body, prefectureCode string, spot *string, visitedOn *time.Time, now time.Time) string {
	if n := utf8.RuneCountInString(strings.TrimSpace(body)); n < 1 || n > maxBodyRunes {
		return "本文は1〜1000文字にしてください"
	}
	if len(prefectureCode) != 2 || prefectureCode < "01" || prefectureCode > "47" {
		return "都道府県を選択してください"
	}
	if spot != nil && utf8.RuneCountInString(*spot) > maxSpotNameRunes {
		return "スポット名は100文字までにしてください"
	}
	// **訪問日は任意である。** 覚えていない・特定の日に紐づかない投稿もある。
	// 省略された投稿は旅行履歴（訪問日順）に出ない。
	if visitedOn != nil {
		// 未来の訪問日は受け付けない。「行った記録」であるため。
		// 日付単位で比較する（時刻を持たない概念のため）。
		if visitedOn.After(now.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)) {
			return "訪問日に未来の日付は指定できません"
		}
	}
	return ""
}

func validateMediaInput(media []gen.PostMediaInput) string {
	if len(media) < 1 || len(media) > maxMediaPerPost {
		return "画像は1〜4枚にしてください"
	}
	seen := make(map[int64]struct{}, len(media))
	for _, m := range media {
		if _, dup := seen[m.MediaId]; dup {
			return "同じ画像を複数回指定することはできません"
		}
		seen[m.MediaId] = struct{}{}
	}
	return ""
}

// normalizeTags はタグを正規化し、重複を取り除く。
//
// 正規化しないと「東北」「東北 」「東北」（全角空白付き）が
// 別のタグとして登録され、タグでの絞り込みが分散する。
func normalizeTags(tags *[]string) ([]string, string) {
	if tags == nil {
		return []string{}, ""
	}
	if len(*tags) > maxTags {
		return nil, "タグは5個までにしてください"
	}

	out := make([]string, 0, len(*tags))
	seen := make(map[string]struct{}, len(*tags))
	for _, raw := range *tags {
		// **NFKC で揃えてから小文字にする。** 順序を逆にしない。
		// 全角の英字は NFKC で半角になるため、先に小文字化しても
		// ＴＯＫＹＯ は ｔｏｋｙｏ にしかならず TOKYO と一致しない。
		//
		// これが無いと ＴＯＫＹＯ と TOKYO、ﾎｯｶｲﾄﾞｳ と ホッカイドウ が
		// **別のタグとして登録される。** タグでの絞り込みは完全一致
		// （search.go の t.name = ?）なので、分裂した側にヒットしない。
		name := strings.ToLower(norm.NFKC.String(strings.TrimSpace(raw)))
		name = strings.TrimPrefix(name, "#")
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > maxTagRunes {
			return nil, "タグは50文字までにしてください"
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, ""
}

func trimOptional(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

// openapiDate は time.Time を仕様上の日付型へ変換する。
//
// 訪問日は「日」の概念であり時刻を持たない。生成された型は
// 日付だけを JSON へ書き出す（YYYY-MM-DD）。
// optionalDate は任意の訪問日を取り出す。未指定なら nil。
//
// **nil と「ゼロ値の日時」を区別する。** 値として持たせると
// 「訪問日が無い」と「西暦1年に行った」の区別が付かなくなる。
func optionalDate(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

// optionalOpenapiDate は任意の訪問日を応答の型に変える。
func optionalOpenapiDate(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}

// ---------------------------------------------------------------------------
// 新着フィード
// ---------------------------------------------------------------------------

// defaultFeedLimit / maxFeedLimit は1回に返す件数。
const (
	defaultFeedLimit = 20
	maxFeedLimit     = 50
)

func (h *postHandler) ListPosts(w http.ResponseWriter, r *http.Request, params gen.ListPostsParams) {
	cursor, ok := parseCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	writeFeedPage(w, r, h.likes, h.avatars, h.logger, params.Limit,
		func(ctx context.Context, limit int) ([]domain.Post, string, error) {
			posts, next, err := h.repo.ListFeed(ctx, cursor, limit, h.storage, displayURLTTL)
			return posts, formatCursor(next), err
		})
}

// ---------------------------------------------------------------------------
// フォロー中フィード
// ---------------------------------------------------------------------------

func (h *postHandler) ListFollowingFeed(w http.ResponseWriter, r *http.Request, params gen.ListFollowingFeedParams) {
	viewerID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	cursor, ok := parseCursor(w, r, params.Cursor)
	if !ok {
		return
	}

	writeFeedPage(w, r, h.likes, h.avatars, h.logger, params.Limit,
		func(ctx context.Context, limit int) ([]domain.Post, string, error) {
			posts, next, err := h.repo.ListFollowingFeed(ctx, viewerID, cursor, limit, h.storage, displayURLTTL)
			return posts, formatCursor(next), err
		})
}

// ---------------------------------------------------------------------------
// 画像の処理状況
// ---------------------------------------------------------------------------

// GetMediaStatus は画像の処理が終わったかを返す。
//
// S3 への送信が終わっても、形式の検証・EXIF の除去・変換が終わるまでは
// 投稿に使えない。その処理は非同期に走るため、クライアントが完了を
// 知る手段としてこの経路を用意している。
func (h *postHandler) GetMediaStatus(w http.ResponseWriter, r *http.Request, mediaID int64) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	rec, err := h.repo.FindMediaByID(r.Context(), uint64(mediaID))
	if errors.Is(err, store.ErrMediaNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "画像が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "画像の記録を取得できない", err)
		return
	}
	// 他人がアップロードした画像の状態は返さない。
	if rec.UserID != userID {
		writeError(w, r, http.StatusForbidden, "forbidden", "この画像を参照する権限がありません")
		return
	}

	var body gen.MediaStatusResponse
	body.Data.MediaId = int64(rec.ID)
	body.Data.Status = gen.MediaStatusResponseDataStatus(rec.Status)
	writeJSON(w, r, http.StatusOK, body.Data)
}
