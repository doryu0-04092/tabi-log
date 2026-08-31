// Package config は環境変数から設定を読み取る。
//
// 設定の読み取りをここ1か所に閉じているのは、「どの環境変数が必要か」を
// コード全体から探し回らずに済ませるためである。値が欠けている場合は
// 起動時に失敗させ、動き始めてから初めて気づく状態を作らない。
package config

import (
	"errors"
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

	// CDN は画像を CloudFront から配るための設定。
	//
	// **空なら S3 の署名付き URL で配る。** ローカルと LocalStack には
	// CloudFront が無いため、そちらは今までどおり動かす必要がある。
	// 「設定があれば CDN、無ければ S3」という分岐は1か所（cmd/server）に閉じる。
	CDN CDNConfig
}

// CDNConfig は CloudFront 経由の配信に必要な3点。
//
// **3つとも揃って初めて有効になる。** 一部だけ設定された状態は
// 設定漏れであり、黙って S3 に落ちると「CDN にしたつもりが効いていない」
// という気づけない状態になる。Load() で弾く。
type CDNConfig struct {
	// Domain は配信ドメイン（例 d111111abcdef8.cloudfront.net）。
	Domain string
	// KeyPairID は CloudFront に登録した公開鍵の ID。
	KeyPairID string
	// PrivateKey は上と対になる秘密鍵（PEM）。
	// 本番では SSM Parameter Store の SecureString から注入する。
	PrivateKey string
	// CookieTTL は署名付き Cookie の有効期間。
	//
	// **アクセストークンの再取得のたびに置き直される**ため、
	// 使い続けている限り切れない。長くしすぎると、漏れたときに
	// 有効な期間が延びるだけである。
	CookieTTL time.Duration
}

// Enabled は CloudFront 経由で配るかどうかを返す。
func (c CDNConfig) Enabled() bool {
	return c.Domain != "" || c.KeyPairID != "" || c.PrivateKey != ""
}

// Complete は3点が揃っているかを返す。
func (c CDNConfig) Complete() bool {
	return c.Domain != "" && c.KeyPairID != "" && c.PrivateKey != ""
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

	// PostCreateLimit / CommentCreateLimit / WriteLimitWindow は
	// 認証済みの利用者が書き込める件数の上限。
	// **ログイン試行とは目的が違う**（internal/httpapi/writelimit.go）。
	PostCreateLimit    int
	CommentCreateLimit int

	// UploadLimit は署名付き URL を発行できる回数。
	//
	// **投稿の上限とは別に要る。** 発行するだけで S3 の PUT・
	// Lambda の起動・行の追加が起きるため、投稿を作らずに
	// 資源を消費できる。投稿1件につき最大4枚なので、
	// 投稿の上限（既定20件/時）より緩い値にしてある。
	UploadLimit      int
	WriteLimitWindow time.Duration

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

	// TrustedProxyHops は X-Forwarded-For の末尾から数えて何個目を発信元とみなすか。
	// CloudFront → ALB → アプリ なので既定は 1。
	// **左端を採ると詐称でレート制限を回避される**(internal/httpapi/auth.go)。
	TrustedProxyHops int
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
	// db.t4g.small の max_connections は既定の式 {DBInstanceClassMemory/12582880}
	// でおよそ 170 である。既定値 25 なら、タスク2つで 50 となり収まる。
	//
	// **確認用の構成は db.t4g.micro（約85）を 1 タスクで使っている。**
	// 25 × 1 = 25 で収まるが、**クラスとタスク数の両方が上限を決める。**
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
// Role はどのプログラムが設定を読むか。
//
// **必要な設定はプログラムごとに違う。** 画像処理はトークンを
// 発行しないし、Cookie も CDN も扱わない。同じ必須条件を課すと、
// **使わない秘密を渡すためだけに配ることになる。**
type Role int

const (
	// RoleServer は API サーバー。すべての設定を使う。
	RoleServer Role = iota

	// RoleImageWorker は画像処理。DB と S3 しか使わない。
	RoleImageWorker
)

// Load は API サーバーとして設定を読む。
func Load() (Config, error) { return LoadFor(RoleServer) }

// LoadFor は役割に応じて設定を読み、その役割に要る条件だけを確かめる。
func LoadFor(role Role) (Config, error) {
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
			PostCreateLimit:    envInt("POST_CREATE_LIMIT", 20),
			CommentCreateLimit: envInt("COMMENT_CREATE_LIMIT", 60),
			UploadLimit:        envInt("UPLOAD_LIMIT", 120),
			WriteLimitWindow:   envDuration("WRITE_LIMIT_WINDOW", time.Hour),
			TrustProxyHeaders:  envBool("TRUST_PROXY_HEADERS", false),
			TrustedProxyHops:   envInt("TRUSTED_PROXY_HOPS", 1),
		},
		Storage: StorageConfig{
			Bucket:         envString("STORAGE_S3_BUCKET", ""),
			Region:         envString("STORAGE_S3_REGION", "ap-northeast-1"),
			Endpoint:       envString("STORAGE_S3_ENDPOINT", ""),
			PublicEndpoint: envString("STORAGE_S3_PUBLIC_ENDPOINT", ""),
			CDN: CDNConfig{
				Domain:     envString("CDN_DOMAIN", ""),
				KeyPairID:  envString("CDN_KEY_PAIR_ID", ""),
				PrivateKey: envString("CDN_PRIVATE_KEY", ""),
				CookieTTL:  envDuration("CDN_COOKIE_TTL", 24*time.Hour),
			},
		},
	}

	var missing []string
	if cfg.DB.User == "" {
		missing = append(missing, "DB_USER")
	}
	if cfg.DB.Password == "" {
		missing = append(missing, "DB_PASSWORD")
	}
	// **画像処理には要らない。** トークンを発行も検証もしない。
	// 必須にすると、使わない秘密を Lambda に配ることになる。
	if role == RoleServer && cfg.Auth.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if cfg.Storage.Bucket == "" {
		missing = append(missing, "STORAGE_S3_BUCKET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("必須の環境変数が設定されていない: %s", strings.Join(missing, ", "))
	}

	// **設定が中途半端なまま起動させない。**
	// 一部だけ設定された状態で S3 に落ちると、「CDN にしたつもりが
	// 効いていない」という、動いてしまうぶん気づけない状態になる。
	if cfg.Storage.CDN.Enabled() && !cfg.Storage.CDN.Complete() {
		return Config{}, errors.New(
			"CDN_DOMAIN・CDN_KEY_PAIR_ID・CDN_PRIVATE_KEY は3つとも設定するか、3つとも空にすること")
	}

	// 短い署名鍵は総当たりで復元されうる。HS256 の出力は 256 ビットなので、
	// 鍵もそれ以上の強度を持たせる。
	if role == RoleServer && len(cfg.Auth.JWTSecret) < minJWTSecretLength {
		return Config{}, fmt.Errorf(
			"JWT_SECRET が短すぎる: %d 文字（%d 文字以上が必要）",
			len(cfg.Auth.JWTSecret), minJWTSecretLength)
	}

	if err := checkProduction(cfg, role); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// checkProduction は本番で成立しない設定を起動時に弾く。
//
// **どれも「動いてしまう」種類の間違いである。** 起動は成功し、
// 画面も一見動く。壊れているのは守りの部分だけなので、
// 気づく機会が無い。**起動を止めるのが唯一の検知手段になる。**
//
// 本番かどうかは APP_ENV で判断する。ここを誤って production に
// してもローカルが起動しなくなるだけで、逆よりはるかに安全である。
func checkProduction(cfg Config, role Role) error {
	// **API サーバーだけの条件である。** 画像処理は Cookie も CDN も
	// 扱わず、発信元も見ない。同じ条件を課すと起動できなくなる。
	if cfg.Env != envProduction || role != RoleServer {
		return nil
	}

	var problems []string

	// Secure が無いと、リフレッシュ Cookie が平文の経路にも送られる。
	if !cfg.Auth.CookieSecure {
		problems = append(problems, "COOKIE_SECURE が false（Cookie が平文の経路にも送られる）")
	}

	// ALB の背後では、これが false だと発信元が全員 ALB の IP になる。
	// **ログイン試行の制限が全利用者で1つの鍵に潰れる。**
	if !cfg.Auth.TrustProxyHeaders {
		problems = append(problems, "TRUST_PROXY_HEADERS が false（発信元が全員 ALB になり制限が潰れる）")
	}

	// LocalStack 向けの指定が残ったまま本番に出ると、S3 に届かない。
	if cfg.Storage.Endpoint != "" {
		problems = append(problems, "STORAGE_S3_ENDPOINT が設定されている（ローカル向けの指定が残っている）")
	}

	// CDN を通さないと、署名付き Cookie による配信の制限が効かない。
	if !cfg.Storage.CDN.Enabled() {
		problems = append(problems, "CDN_DOMAIN が空（画像が CloudFront を通らない）")
	}

	if len(problems) > 0 {
		return fmt.Errorf("APP_ENV=production では成立しない設定がある: %s",
			strings.Join(problems, " / "))
	}
	return nil
}

// minJWTSecretLength は署名鍵に要求する最小の長さ。
const minJWTSecretLength = 32

// envProduction は本番を表す APP_ENV の値。
// **infra/ecs.tf の環境変数と揃っている必要がある。**
const envProduction = "production"

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
