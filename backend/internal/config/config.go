// Package config は環境変数から設定を読み取る。
//
// 設定の読み取りをここ1か所に閉じているのは、「どの環境変数が必要か」を
// コード全体から探し回らずに済ませるためである。値が欠けている場合は
// 起動時に失敗させ、動き始めてから初めて気づく状態を作らない。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config はアプリケーション全体の設定を表す。
type Config struct {
	Env             string // local / production
	Port            int
	LogLevel        string // debug / info / warn / error
	ShutdownTimeout time.Duration

	DB      DBConfig
	Auth    AuthConfig
	Storage StorageConfig
}

// StorageConfig は画像の保存先の設定を表す。
type StorageConfig struct {
	Bucket string
	Region string

	// Endpoint は LocalStack を使うときだけ設定する。
	//
	// **既定を空にしているのが要点である。** 前回プロジェクトでは既定値が
	// ローカル向けのアドレスになっており、本番のタスク定義で空文字を渡し忘れると
	// 到達できないアドレスへ接続しに行って起動に失敗する罠になっていた。
	// 既定を「実際の S3」にしておけば、設定漏れは安全側に倒れる。
	Endpoint string

	// PublicEndpoint は署名付き URL に使うアドレス。ローカルのみ設定する。
	// 詳細は storage.S3Config を参照。
	PublicEndpoint string
}

// AuthConfig は認証まわりの設定を表す。
type AuthConfig struct {
	// JWTSecret はアクセストークンの署名鍵。
	// 本番では SSM Parameter Store の SecureString から注入する。
	JWTSecret string

	// JWTKeyID は JWT ヘッダーの kid に入る。
	//
	// 署名鍵を入れ替えるとき、新旧の鍵を並行して検証できるようにするための識別子である。
	// 現在は1つしか無いが、**後から入れるとそれ以前に発行したトークンが
	// どの鍵のものか分からなくなる**ため最初から付けておく。
	JWTKeyID string

	// AccessTokenTTL は短く保つ。
	// 発行済みのアクセストークンは失効させられないため、
	// ログアウト後に有効なまま残る時間がそのままこの値になる。
	AccessTokenTTL time.Duration

	RefreshTokenTTL time.Duration

	// RefreshGracePeriod は、正規のローテーション直後に旧トークンが
	// 提示された場合に盗用と判定しない猶予時間である。
	//
	// タブを複数開いた利用者は同じトークンで同時にリフレッシュを試みる。
	// これが 0 だと後発のリクエストが盗用と判定され、
	// **正常な利用者が突然全ログアウトされる。**
	//
	// 代償として、この時間内に限り盗まれたトークンの再提示も通る。
	// 短くするほど安全だが、遅いネットワークでの同時リフレッシュを
	// 誤検知しやすくなる。
	RefreshGracePeriod time.Duration

	// CookieSecure は本番で true にする。
	// ローカルは HTTP のため false でないと Cookie が送られない。
	CookieSecure bool

	// LoginAttemptLimit / LoginAttemptWindow はログイン試行の上限。
	LoginAttemptLimit  int
	LoginAttemptWindow time.Duration

	// TrustProxyHeaders は X-Forwarded-For を信用するかどうか。
	//
	// **ALB や CloudFront の背後では true にする。** false のままだと
	// 発信元がすべてロードバランサのアドレスになり、レート制限が
	// 「利用者ごとに N 回」ではなく「全体で N 回」になってしまう。
	//
	// 逆に、プロキシの背後でないのに true にすると、**攻撃者がヘッダーを
	// 詐称してレート制限を回避できる。** どちらの向きにも誤ると壊れるため、
	// 既定値には安全側（false）を置き、環境ごとに明示する。
	TrustProxyHeaders bool
}

// DBConfig はデータベース接続の設定を表す。
type DBConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string

	// MaxOpenConns はこのプロセスが同時に開く接続の上限である。
	//
	// 「タスク数 × MaxOpenConns <= MySQL の max_connections」を満たす必要がある。
	// 既定値 25 は、ECS のタスク2つで 50 となり db.t4g.small の上限（約225）に収まる。
	// タスク数を増やす場合はこの式が上限の根拠になる。
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DSN は go-sql-driver/mysql 用の接続文字列を返す。
//
// parseTime=true は DATETIME を time.Time として受け取るために必須である。
// loc と time_zone を UTC に固定しているのは、アプリケーションが日時を UTC で
// 扱う方針であるため。サーバーのタイムゾーン設定に結果が左右されないようにする。
func (d DBConfig) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=UTC&time_zone=%%27%%2B00%%3A00%%27&charset=utf8mb4&collation=utf8mb4_0900_ai_ci",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

// Load は環境変数から設定を読み取る。必須の値が欠けている場合はエラーを返す。
func Load() (Config, error) {
	cfg := Config{
		Env:             envString("APP_ENV", "local"),
		Port:            envInt("PORT", 8080),
		LogLevel:        envString("LOG_LEVEL", "info"),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		DB: DBConfig{
			Host:            envString("DB_HOST", "localhost"),
			Port:            envInt("DB_PORT", 3306),
			Name:            envString("DB_NAME", "tabilog"),
			User:            envString("DB_USER", ""),
			Password:        envString("DB_PASSWORD", ""),
			MaxOpenConns:    envInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    envInt("DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime: envDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Auth: AuthConfig{
			JWTSecret:          envString("JWT_SECRET", ""),
			JWTKeyID:           envString("JWT_KEY_ID", "v1"),
			AccessTokenTTL:     envDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL:    envDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour),
			RefreshGracePeriod: envDuration("REFRESH_GRACE_PERIOD", 10*time.Second),
			CookieSecure:       envBool("COOKIE_SECURE", false),
			LoginAttemptLimit:  envInt("LOGIN_ATTEMPT_LIMIT", 10),
			LoginAttemptWindow: envDuration("LOGIN_ATTEMPT_WINDOW", 5*time.Minute),
			TrustProxyHeaders:  envBool("TRUST_PROXY_HEADERS", false),
		},
		Storage: StorageConfig{
			Bucket:         envString("STORAGE_S3_BUCKET", ""),
			Region:         envString("STORAGE_S3_REGION", "ap-northeast-1"),
			Endpoint:       envString("STORAGE_S3_ENDPOINT", ""),
			PublicEndpoint: envString("STORAGE_S3_PUBLIC_ENDPOINT", ""),
		},
	}

	var missing []string
	if cfg.DB.User == "" {
		missing = append(missing, "DB_USER")
	}
	if cfg.DB.Password == "" {
		missing = append(missing, "DB_PASSWORD")
	}
	if cfg.Auth.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if cfg.Storage.Bucket == "" {
		missing = append(missing, "STORAGE_S3_BUCKET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("必須の環境変数が設定されていない: %s", strings.Join(missing, ", "))
	}

	// 短い署名鍵は総当たりで復元されうる。HS256 の出力は 256 ビットなので、
	// 鍵もそれ以上の強度を持たせる。
	if len(cfg.Auth.JWTSecret) < minJWTSecretLength {
		return Config{}, fmt.Errorf(
			"JWT_SECRET が短すぎる: %d 文字（%d 文字以上が必要）",
			len(cfg.Auth.JWTSecret), minJWTSecretLength)
	}

	return cfg, nil
}

// minJWTSecretLength は署名鍵に要求する最小の長さ。
const minJWTSecretLength = 32

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
