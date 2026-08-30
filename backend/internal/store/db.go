// Package store はデータベースへの接続とデータアクセスを担う。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/config"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"

	mysqldriver "github.com/go-sql-driver/mysql" // ドライバの登録と、エラー番号の判定に使う
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
// mysqlErrDeadlock / mysqlErrLockWaitTimeout は「やり直せば通る」種類のエラー。
//
// **これらは異常ではなく、並行制御の結果として正常に起こる。**
// InnoDB は循環待ちを検出すると、片方を殺して残りを進める。
// 殺された側は最初からやり直せばよい（MySQL のメッセージ自体が
// "try restarting transaction" と言っている）。
const (
	mysqlErrDeadlock        = 1213
	mysqlErrLockWaitTimeout = 1205
)

// txMaxAttempts は再試行を含めた試行回数の上限。
//
// **無制限にしない。** 常に衝突する設計上の問題を、再試行で覆い隠して
// しまうと、負荷が上がるほど遅くなるだけの状態に気づけなくなる。
const txMaxAttempts = 3

// isRetryableTxError はやり直して意味のあるエラーかを判定する。
func isRetryableTxError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == mysqlErrDeadlock || mysqlErr.Number == mysqlErrLockWaitTimeout
}

// runTx はトランザクションを1回ぶん実行する。
func runTx(ctx context.Context, db *sql.DB, q *dbgen.Queries, fn func(*dbgen.Queries) error) error {
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

func inTx(ctx context.Context, db *sql.DB, q *dbgen.Queries, fn func(*dbgen.Queries) error) error {
	var err error
	for attempt := 1; attempt <= txMaxAttempts; attempt++ {
		err = runTx(ctx, db, q, fn)
		if err == nil || !isRetryableTxError(err) {
			return err
		}

		// **待ってから戻す。** 即座にやり直すと、衝突した相手と再び
		// 同じ瞬間に取りに行き、同じ形で衝突しやすい。
		// 試行ごとに待ち時間を伸ばして、ぶつかる位置をずらす。
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 10 * time.Millisecond):
		}
	}
	return err
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
