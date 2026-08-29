// Package store はデータベースへの接続とデータアクセスを担う。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/config"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"

	_ "github.com/go-sql-driver/mysql" // database/sql に mysql ドライバを登録する
)

// Open はコネクションプールを構成した *sql.DB を返す。
//
// database/sql の Open は接続を確立しないため、ここで一度 Ping して
// 「設定は読めたが実は繋がらない」状態のまま起動が完了することを防ぐ。
func Open(ctx context.Context, cfg config.DBConfig) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("データベース接続の初期化に失敗した: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	// ConnMaxLifetime を設定しないと、ロードバランサや MySQL 側が
	// 一方的に切断した接続をプールが保持し続け、
	// 利用時に初めて失敗する形になる。
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("データベースへ疎通できない: %w", err)
	}

	return db, nil
}

// inTx はトランザクションの開始・巻き戻し・確定をまとめる。
//
// **2つの store が同じ定型を持っていたため関数に切り出した。**
// 巻き戻しの書き忘れは接続を握ったまま離さない不具合になり、見つけにくい。
func inTx(ctx context.Context, db *sql.DB, q *dbgen.Queries, fn func(*dbgen.Queries) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("トランザクションを開始できない: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(q.WithTx(tx)); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("変更を確定できない: %w", err)
	}
	return nil
}

// nullTimeToPtr は NULL 許容の日時をポインタに変える。
//
// **NULL と「ゼロ値の日時」を区別するために使う。** time.Time のゼロ値は
// 西暦1年であり、値として持たせると「訪問日が無い」と「西暦1年に行った」の
// 区別が付かなくなる。
func nullTimeToPtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

// timeToNullTime はポインタを NULL 許容の日時に変える。
func timeToNullTime(v *time.Time) sql.NullTime {
	if v == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *v, Valid: true}
}
