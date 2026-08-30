package store

import (
	"errors"
	"fmt"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// やり直して意味のあるエラーかの判定。
//
// **ここを誤ると、どちらの向きにも壊れる。**
// 広すぎると「毎回失敗するもの」を3回繰り返して遅くなるだけになり、
// 狭すぎると並行制御の正常な結果を利用者に 500 として見せることになる。
//
// 実際に AWS 上の負荷試験で、いいねの同時実行が 38 件の 500 になった(2026-08-30)。
//
//	Error 1213 (40001): Deadlock found when trying to get lock; try restarting transaction
func TestIsRetryableTxError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "デッドロックはやり直す",
			err:  &mysqldriver.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock"},
			want: true,
		},
		{
			name: "ロック待ちの時間切れもやり直す",
			err:  &mysqldriver.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"},
			want: true,
		},
		{
			// **やり直しても同じ結果になるものは対象にしない。**
			// 重複キーは何度試しても重複のままで、待つだけ無駄である。
			name: "重複キーはやり直さない",
			err:  &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry"},
			want: false,
		},
		{
			name: "構文エラーはやり直さない",
			err:  &mysqldriver.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax"},
			want: false,
		},
		{
			name: "MySQL 以外のエラーはやり直さない",
			err:  errors.New("なにか別の失敗"),
			want: false,
		},
		{
			name: "nil はやり直さない",
			err:  nil,
			want: false,
		},
		{
			// **包まれていても見つける。** store の各関数は fmt.Errorf で
			// 文脈を足してから返すため、素の型では届かない。
			name: "包まれたデッドロックも見つける",
			err: fmt.Errorf("いいね数の更新に失敗した: %w",
				&mysqldriver.MySQLError{Number: 1213, Message: "Deadlock found"}),
			want: true,
		},
		{
			name: "二重に包まれていても見つける",
			err: fmt.Errorf("外側: %w", fmt.Errorf("内側: %w",
				&mysqldriver.MySQLError{Number: 1213, Message: "Deadlock found"})),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableTxError(tt.err); got != tt.want {
				t.Fatalf("isRetryableTxError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// 試行回数の上限。
//
// **無制限にしない。** 常に衝突する設計上の問題を再試行で覆い隠すと、
// 負荷が上がるほど遅くなるだけの状態に気づけなくなる。
func TestTxMaxAttempts(t *testing.T) {
	if txMaxAttempts < 2 {
		t.Fatalf("再試行が1回も起きない: %d", txMaxAttempts)
	}
	if txMaxAttempts > 5 {
		t.Fatalf("多すぎる。失敗を覆い隠すことになる: %d", txMaxAttempts)
	}
}
