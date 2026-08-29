// Command server は tabi-log の API サーバーを起動する。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/config"
	"github.com/doryu0-04092/tabi-log/backend/internal/httpapi"
	"github.com/doryu0-04092/tabi-log/backend/internal/search"
	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	if err := run(); err != nil {
		// logger の初期化前に失敗する可能性があるため標準エラーへ直接出す。
		fmt.Fprintf(os.Stderr, "起動に失敗した: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// 署名鍵は現在1つだが、kid で引く形にしておく。
	// 署名方式を変えるときは、ここに検証用の鍵を足してから発行側を切り替える。
	activeKey := auth.SigningKey{
		ID:     cfg.Auth.JWTKeyID,
		Method: jwt.SigningMethodHS256,
		Secret: []byte(cfg.Auth.JWTSecret),
	}
	tokens, err := auth.NewJWTService(activeKey, []auth.SigningKey{activeKey}, cfg.Auth.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("トークンサービスを初期化できない: %w", err)
	}

	s3Storage, err := storage.NewS3Storage(ctx, storage.S3Config{
		Bucket:         cfg.Storage.Bucket,
		Region:         cfg.Storage.Region,
		Endpoint:       cfg.Storage.Endpoint,
		PublicEndpoint: cfg.Storage.PublicEndpoint,
	})
	if err != nil {
		return fmt.Errorf("画像の保存先を初期化できない: %w", err)
	}

	// **保存と配信を分ける。**
	//
	// アップロードと削除は必ず S3 を直接叩く。配信だけは、設定があれば
	// CloudFront に切り替える。切り替えの判断はここ1か所に閉じており、
	// store と httpapi は「表示用 URL を作れるもの」としか見ていない。
	//
	// ローカルと LocalStack には CloudFront が無いため、
	// 設定が空なら今までどおり S3 の署名付き URL で配る。
	var cdnSigner *storage.CDNSigner
	displayURLs := storage.URLSigner(s3Storage)
	if cfg.Storage.CDN.Complete() {
		cdnSigner, err = storage.NewCDNSigner(
			cfg.Storage.CDN.Domain, cfg.Storage.CDN.KeyPairID, cfg.Storage.CDN.PrivateKey)
		if err != nil {
			return fmt.Errorf("画像配信の署名鍵を読み込めない: %w", err)
		}
		displayURLs = cdnSigner
		logger.Info("画像は CloudFront から配る", slog.String("domain", cfg.Storage.CDN.Domain))
	} else {
		// **黙って S3 に落ちないよう、どちらで動いているかを残す。**
		logger.Info("画像は S3 の署名付き URL で配る（CDN の設定なし）")
	}

	objectStorage := mediaStorage{S3Storage: s3Storage, display: displayURLs}

	handler := httpapi.NewRouter(httpapi.Deps{
		DB:            db,
		Prefectures:   store.NewPrefectureStore(db),
		Auth:          store.NewAuthStore(db),
		Posts:         store.NewPostStore(db),
		Reactions:     store.NewReactionStore(db),
		Follows:       store.NewFollowStore(db),
		Search:        search.NewMySQLSearcher(db).WithLogger(logger),
		Notifications: store.NewNotificationStore(db),
		Account:       store.NewAccountStore(db),
		Storage:       objectStorage,
		CDNCookies:    cdnSigner,
		CDNCookieTTL:  cfg.Storage.CDN.CookieTTL,
		TokenIssuer:   tokens,
		TokenVerifier: tokens,
		AuthOptions: httpapi.AuthOptions{
			RefreshTTL:   cfg.Auth.RefreshTokenTTL,
			RefreshGrace: cfg.Auth.RefreshGracePeriod,
			CookieSecure: cfg.Auth.CookieSecure,
			// ローカルは直接受けるので false。ALB/CloudFront の背後では
			// TRUST_PROXY_HEADERS=true にしないと、全利用者が同じ発信元と
			// みなされてレート制限が機能しなくなる。
			TrustProxyHeaders: cfg.Auth.TrustProxyHeaders,
		},
		LoginAttemptLimit:  cfg.Auth.LoginAttemptLimit,
		PostCreateLimit:    cfg.Auth.PostCreateLimit,
		CommentCreateLimit: cfg.Auth.CommentCreateLimit,
		WriteLimitWindow:   cfg.Auth.WriteLimitWindow,
		LoginAttemptWindow: cfg.Auth.LoginAttemptWindow,
		Logger:             logger,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("サーバーを起動する",
			slog.String("env", cfg.Env),
			slog.Int("port", cfg.Port),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("サーバーが異常終了した: %w", err)
	case <-ctx.Done():
		// 停止シグナルを受けたら、処理中のリクエストを打ち切らずに待つ。
		// ECS はタスク停止時に SIGTERM を送ってから一定時間後に SIGKILL するため、
		// この猶予の内に片付ける。
		logger.Info("停止シグナルを受信した。処理中のリクエストの完了を待つ",
			slog.Duration("timeout", cfg.ShutdownTimeout),
		)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("停止処理がタイムアウトした: %w", err)
		}
		logger.Info("停止した")
		return nil
	}
}

// newLogger は構造化 JSON を標準出力へ書く logger を返す。
//
// 出力先を標準出力のみにしているのは、アプリケーションがログの送り先を
// 知らない状態を保つためである。収集先（CloudWatch Logs など）を変えても
// アプリケーションを変更せずに済む。ファイルには書かない。
func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

// mediaStorage は保存（S3）と配信（S3 または CloudFront）を組み合わせる。
//
// **アップロードと削除は S3 でしか行えない。** CloudFront は読むためのものである。
// 埋め込みで S3Storage の PresignPut と Delete をそのまま使い、
// 表示用 URL だけを差し替える。
type mediaStorage struct {
	*storage.S3Storage
	display storage.URLSigner
}

func (m mediaStorage) DisplayURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return m.display.DisplayURL(ctx, key, ttl)
}
