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

	DB DBConfig
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
	}

	var missing []string
	if cfg.DB.User == "" {
		missing = append(missing, "DB_USER")
	}
	if cfg.DB.Password == "" {
		missing = append(missing, "DB_PASSWORD")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("必須の環境変数が設定されていない: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
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
