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
	GetPost(ctx context.Context, postID uint64, signer storage.URLSigner, ttl time.Duration) (domain.Post, error)
}

// ObjectStorage は画像の保存先。
type ObjectStorage interface {
	storage.URLSigner
	PresignPut(ctx context.Context, key, contentType string, contentLength int64, ttl time.Duration) (string, error)
	Delete(ctx context.Context, keys ...string) error
}

type postHandler struct {
	repo    PostRepository
	storage ObjectStorage
	logger  *slog.Logger
	now     func() time.Time
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

	var req gen.CreatePostRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	spot := trimOptional(req.SpotName)
	tags, msg := normalizeTags(req.Tags)
	if msg == "" {
		msg = validatePostFields(req.Body, req.PrefectureCode, spot, req.VisitedOn.Time, h.now())
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
		media = append(media, store.MediaAttachment{
			MediaID: uint64(m.MediaId),
			AltText: strings.TrimSpace(m.AltText),
		})
	}

	postID, err := h.repo.CreatePost(r.Context(), store.CreatePostInput{
		UserID:         userID,
		Body:           strings.TrimSpace(req.Body),
		PrefectureCode: req.PrefectureCode,
		SpotName:       spot,
		VisitedOn:      req.VisitedOn.Time,
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

	h.respondWithPost(w, r, postID, http.StatusCreated)
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
		msg = validatePostFields(req.Body, req.PrefectureCode, spot, req.VisitedOn.Time, h.now())
	}
	if msg == "" {
		msg = validateAltTexts(req.MediaAltTexts)
	}
	if msg != "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", msg)
		return
	}

	alts := make([]store.MediaAttachment, 0, len(req.MediaAltTexts))
	for _, m := range req.MediaAltTexts {
		alts = append(alts, store.MediaAttachment{
			MediaID: uint64(m.MediaId),
			AltText: strings.TrimSpace(m.AltText),
		})
	}

	if err := h.repo.UpdatePost(r.Context(), store.UpdatePostInput{
		PostID:         uint64(postID),
		UserID:         userID,
		Body:           strings.TrimSpace(req.Body),
		PrefectureCode: req.PrefectureCode,
		SpotName:       spot,
		VisitedOn:      req.VisitedOn.Time,
		Tags:           tags,
		AltTexts:       alts,
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
	// 残ったオブジェクトはライフサイクルルールで期限削除される。
	if err := h.storage.Delete(r.Context(), keys...); err != nil {
		h.logger.ErrorContext(r.Context(), "投稿は削除したがS3のオブジェクトを削除できなかった",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Uint64("post_id", uint64(postID)),
			slog.Int("key_count", len(keys)),
			slog.String("error", err.Error()),
		)
	}

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
	post, err := h.repo.GetPost(r.Context(), postID, h.storage, displayURLTTL)
	if errors.Is(err, store.ErrPostNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "投稿が見つかりません")
		return
	}
	if err != nil {
		h.internalError(w, r, "投稿の取得に失敗した", err)
		return
	}
	writeJSON(w, r, status, toAPIPost(post))
}

func (h *postHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.logger.ErrorContext(r.Context(), msg,
		slog.String("request_id", RequestIDFrom(r.Context())),
		slog.String("error", err.Error()),
	)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}

func toAPIPost(p domain.Post) gen.Post {
	media := make([]gen.Media, 0, len(p.Media))
	for _, m := range p.Media {
		media = append(media, gen.Media{
			Id:        int64(m.ID),
			AltText:   m.AltText,
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
		VisitedOn:    openapiDate(p.VisitedOn),
		Media:        media,
		Tags:         tags,
		LikeCount:    p.LikeCount,
		CommentCount: p.CommentCount,
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

func validatePostFields(body, prefectureCode string, spot *string, visitedOn, now time.Time) string {
	if n := utf8.RuneCountInString(strings.TrimSpace(body)); n < 1 || n > maxBodyRunes {
		return "本文は1〜1000文字にしてください"
	}
	if len(prefectureCode) != 2 || prefectureCode < "01" || prefectureCode > "47" {
		return "都道府県を選択してください"
	}
	if spot != nil && utf8.RuneCountInString(*spot) > maxSpotNameRunes {
		return "スポット名は100文字までにしてください"
	}
	if visitedOn.IsZero() {
		return "訪問日を入力してください"
	}
	// 未来の訪問日は受け付けない。「行った記録」であるため。
	// 日付単位で比較する（時刻を持たない概念のため）。
	if visitedOn.After(now.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)) {
		return "訪問日に未来の日付は指定できません"
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
		if msg := validateAltText(m.AltText); msg != "" {
			return msg
		}
	}
	return ""
}

func validateAltTexts(alts []gen.MediaAltText) string {
	for _, m := range alts {
		if msg := validateAltText(m.AltText); msg != "" {
			return msg
		}
	}
	return ""
}

// validateAltText は代替テキストを検証する。
//
// **空を許さない。** 写真が主役のサービスで代替テキストを任意にすると
// 実質的に入力されず、画像が見えない利用者に何も伝わらなくなる。
func validateAltText(alt string) string {
	n := utf8.RuneCountInString(strings.TrimSpace(alt))
	if n < 1 {
		return "画像の説明（代替テキスト）を入力してください"
	}
	if n > maxAltTextRunes {
		return "画像の説明は200文字までにしてください"
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
		name := strings.ToLower(strings.TrimSpace(raw))
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
func openapiDate(t time.Time) openapi_types.Date {
	return openapi_types.Date{Time: t}
}
