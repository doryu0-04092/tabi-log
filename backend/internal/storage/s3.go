package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config は S3 クライアントの設定。
type S3Config struct {
	Bucket string
	Region string

	// Endpoint は LocalStack を使うときだけ設定する。
	// このプロセスから S3 の API を呼ぶときのアドレスである。
	//
	// **本番では必ず空にすること。** 空でないと実際の S3 ではなく
	// 到達できないアドレスへ接続しに行き、起動に失敗する。
	// 前回プロジェクトで、既定値がローカル向けだったために
	// デプロイ時に踏んだ罠であり、今回は既定を空にしている。
	Endpoint string

	// PublicEndpoint は署名付き URL に使うアドレス。空なら Endpoint を使う。
	//
	// **ローカルでは Endpoint と別の値が必要になる。** バックエンドは
	// Docker ネットワーク内から `http://localstack:4566` へ接続するが、
	// 署名付き URL を受け取るのはホスト側のブラウザであり、
	// そのホスト名は解決できない。
	//
	// 単にホスト名を差し替えるだけでは通らない。**SigV4 は Host ヘッダーを
	// 署名に含める**ため、署名した宛先と実際の接続先が食い違うと
	// 署名の検証に失敗する。そこで署名専用のクライアントを
	// 公開アドレスで作り、最初からそのホスト名で署名する。
	//
	// 本番では S3 の実エンドポイントがどこからでも到達できるため、
	// この区別は不要になる（両方とも空になる）。
	PublicEndpoint string
}

// S3Storage は Amazon S3（および LocalStack）へ保存する。
type S3Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

// NewS3Storage を作る。
func NewS3Storage(ctx context.Context, cfg S3Config) (*S3Storage, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("バケット名が空である")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("AWS の設定を読み込めない: %w", err)
	}

	withEndpoint := func(endpoint string) func(*s3.Options) {
		return func(o *s3.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
				// LocalStack は仮想ホスト形式の名前解決ができないため、
				// パス形式（endpoint/bucket/key）を使う。
				o.UsePathStyle = true
			}
		}
	}

	// API 呼び出し用。このプロセスから到達できるアドレスを使う。
	client := s3.NewFromConfig(awsCfg, withEndpoint(cfg.Endpoint))

	// 署名用。**利用者のブラウザから到達できるアドレスで署名する。**
	publicEndpoint := cfg.PublicEndpoint
	if publicEndpoint == "" {
		publicEndpoint = cfg.Endpoint
	}
	presignBase := client
	if publicEndpoint != cfg.Endpoint {
		presignBase = s3.NewFromConfig(awsCfg, withEndpoint(publicEndpoint))
	}

	return &S3Storage{
		client:    client,
		presigner: s3.NewPresignClient(presignBase),
		bucket:    cfg.Bucket,
	}, nil
}

func (s *S3Storage) PresignPut(ctx context.Context, key, contentType string, contentLength int64, ttl time.Duration) (string, error) {
	req, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		// 署名に含めることで、異なる種類・大きさのアップロードを
		// S3 側で拒否させる。クライアントの善意に頼らない。
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("アップロード用URLの発行に失敗した: %w", err)
	}
	return req.URL, nil
}

func (s *S3Storage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("表示用URLの発行に失敗した: %w", err)
	}
	return req.URL, nil
}

func (s *S3Storage) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	objects := make([]types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		objects = append(objects, types.ObjectIdentifier{Key: aws.String(k)})
	}

	// 存在しないキーを含んでいてもエラーにならない（S3 の削除は冪等）。
	_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("オブジェクトの削除に失敗した: %w", err)
	}
	return nil
}
