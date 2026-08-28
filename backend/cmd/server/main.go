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

	"github.com/doryu0-04092/tabi-log/backend/internal/config"
	"github.com/doryu0-04092/tabi-log/backend/internal/httpapi"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
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

	handler := httpapi.NewRouter(httpapi.Deps{DB: db, Logger: logger})

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
