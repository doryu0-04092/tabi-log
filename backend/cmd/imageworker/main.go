// Command imageworker は S3 に置かれた画像を検証・変換する。
//
// **バックエンドは画像の中身を一度も見ない。** ブラウザから S3 へ直接
// アップロードさせているためである（大きなファイルでサーバーの帯域と
// タイムアウトがボトルネックになるのを避ける）。その結果、
// **中身を検証できる唯一の場所がこの処理になる**。
//
// AWS では S3 の PutObject イベントで起動する Lambda として動く。
// ローカルでは LocalStack が同じ経路で起動する。
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/doryu0-04092/tabi-log/backend/internal/config"
	"github.com/doryu0-04092/tabi-log/backend/internal/media"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// originalsPrefix / variantsPrefix はバックエンドと揃える。
const (
	originalsPrefix = "originals/"
	variantsPrefix  = "variants/"
)

type worker struct {
	s3     *s3.Client
	bucket string
	store  *store.PostStore
	logger *slog.Logger
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	w, err := newWorker(context.Background(), logger)
	if err != nil {
		logger.Error("初期化に失敗した", slog.String("error", err.Error()))
		os.Exit(1)
	}

	lambda.Start(w.handle)
}

func newWorker(ctx context.Context, logger *slog.Logger) (*worker, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	db, err := store.Open(ctx, cfg.DB)
	if err != nil {
		return nil, err
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Storage.Region))
	if err != nil {
		return nil, fmt.Errorf("AWS の設定を読み込めない: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Storage.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Storage.Endpoint)
			o.UsePathStyle = true
		}
	})

	return &worker{s3: client, bucket: cfg.Storage.Bucket, store: store.NewPostStore(db), logger: logger}, nil
}

// handle は S3 のイベントを処理する。
//
// **1件でも失敗したらエラーを返す。** Lambda は失敗したイベントを再試行するため、
// 一時的な障害（データベースの瞬断など）はそれで回復する。
// 回復しない失敗（画像として読めない等）は media を failed にしてから
// 正常終了させ、無限の再試行を避ける。
func (w *worker) handle(ctx context.Context, event events.S3Event) error {
	for _, rec := range event.Records {
		key, err := url.QueryUnescape(rec.S3.Object.Key)
		if err != nil {
			// キーが壊れているイベントは再試行しても直らない。
			w.logger.Error("オブジェクトキーを復号できない",
				slog.String("raw_key", rec.S3.Object.Key), slog.String("error", err.Error()))
			continue
		}

		// 変換物の書き込みで再びイベントが起きる。**自分の出力を処理しない。**
		// これを見落とすと、変換物をさらに変換し続ける無限の連鎖になる。
		if !strings.HasPrefix(key, originalsPrefix) {
			continue
		}

		if err := w.processOne(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) processOne(ctx context.Context, key string) error {
	log := w.logger.With(slog.String("s3_key", key))

	// キーから記録を引けるということは、**このアプリケーションが発行した
	// 署名付き URL 経由で置かれた**ことを意味する。引けないものは処理しない。
	rec, err := w.store.FindMediaByS3Key(ctx, key)
	if errors.Is(err, store.ErrMediaNotFound) {
		log.Warn("記録の無いオブジェクトを無視した")
		return nil
	}
	if err != nil {
		return fmt.Errorf("画像の記録を引けない: %w", err)
	}

	// 既に処理済みなら何もしない。Lambda は同じイベントを2回配送しうる。
	if rec.Status == "processed" {
		log.Info("既に処理済みのため何もしない", slog.Uint64("media_id", rec.ID))
		return nil
	}

	data, err := w.download(ctx, key)
	if err != nil {
		return fmt.Errorf("オブジェクトを取得できない: %w", err)
	}

	result, err := media.Process(data)
	if err != nil {
		// 画像として扱えないものは再試行しても直らない。
		// **failed として記録し、正常終了する。** 記録を残すのは、
		// 投稿に使えない理由を説明でき、後から調査できるようにするためである。
		log.Warn("画像として処理できなかった",
			slog.Uint64("media_id", rec.ID), slog.String("error", err.Error()))
		if err := w.store.FailMediaProcessing(ctx, rec.ID); err != nil {
			return fmt.Errorf("失敗状態を記録できない: %w", err)
		}
		return nil
	}

	variants := make([]store.ProcessedVariant, 0, len(result.Variants))
	for _, v := range result.Variants {
		variantKey := variantKeyFor(key, v.Kind)
		if err := w.upload(ctx, variantKey, v.Data); err != nil {
			return fmt.Errorf("%s の書き込みに失敗した: %w", v.Kind, err)
		}
		variants = append(variants, store.ProcessedVariant{
			Kind:   v.Kind,
			S3Key:  variantKey,
			Width:  v.Width,
			Height: v.Height,
			Bytes:  len(v.Data),
		})
	}

	// **変換物を先に置いてから processed にする。**
	// 逆にすると、変換物がまだ無い画像が投稿に使われ、
	// 表示できない画像を含む投稿ができてしまう。
	if err := w.store.CompleteMediaProcessing(ctx, store.ProcessedMedia{
		MediaID:  rec.ID,
		Mime:     result.Mime,
		Width:    result.Width,
		Height:   result.Height,
		Bytes:    len(data),
		Variants: variants,
	}); err != nil {
		return fmt.Errorf("処理の完了を記録できない: %w", err)
	}

	log.Info("画像を処理した",
		slog.Uint64("media_id", rec.ID),
		slog.String("mime", result.Mime),
		slog.Int("width", result.Width),
		slog.Int("height", result.Height),
	)
	return nil
}

// variantKeyFor は原本のキーから変換物のキーを作る。
//
// 接頭辞を分けているのは、**原本だけをライフサイクルルールの対象に
// できるようにする**ためである。変換物は投稿と一緒に消す。
func variantKeyFor(originalKey, kind string) string {
	base := strings.TrimSuffix(path.Base(originalKey), path.Ext(originalKey))
	// 変換物は常に JPEG である（webp の書き出しが標準ライブラリに無いため統一）。
	return variantsPrefix + base + "_" + kind + ".jpg"
}

func (w *worker) download(ctx context.Context, key string) ([]byte, error) {
	out, err := w.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Body.Close() }()

	// 読み取り量に上限を設ける。署名付き URL には最大サイズを焼き込んでいるが、
	// **この処理はそれを前提にせず、自分で守る。**
	const maxBytes = 10 * 1024 * 1024
	return io.ReadAll(io.LimitReader(out.Body, maxBytes+1))
}

func (w *worker) upload(ctx context.Context, key string, data []byte) error {
	_, err := w.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(w.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("image/jpeg"),
	})
	return err
}
