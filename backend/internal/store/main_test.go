package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
)

/*
store パッケージのテストは**実際の MySQL に対して実行する。**

理由は2つある。

  1. この層に書いてあるのは Go のロジックではなく **SQL** である。
     偽の実装を相手にすると、検証しているのは「Go の呼び出しが通ること」
     だけになり、SQL が意図どおりかは何も分からない
  2. sqlc の制約で書き換えた箇所（行値比較の展開、UPDATE ... JOIN の
     相関副問い合わせ化）や、索引が効いているかは、**実行しないと分からない**

過去に実際、いいね・コメントのカウンタが退会時にずれる不具合と、
カーソル問い合わせで filesort が発生する事象を、いずれも
実環境で叩いて初めて見つけている。

---

# 実行方法

接続先は環境変数 TEST_DB_DSN で渡す。

	docker compose up -d mysql
	docker compose exec -T mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" \
	  -e "CREATE DATABASE IF NOT EXISTS tabilog_test
	      CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
	      GRANT ALL ON tabilog_test.* TO 'tabilog'@'%';"
	DB_NAME=tabilog_test docker compose run --rm migrate up

	cd backend
	TEST_DB_DSN='tabilog:change_me_local_only@tcp(127.0.0.1:3306)/tabilog_test?parseTime=true&loc=UTC&multiStatements=true' \
	  go test ./internal/store/...

**スキーマの用意にマイグレーションをそのまま使う。** テスト側で
CREATE TABLE を書くと本番のスキーマと二重管理になり、
「テストは通るが本番では動かない」が起きる。
*/

// testDB はテスト全体で共有する接続。TestMain が用意する。
var testDB *sql.DB

// skipReason が空でなければ、各テストはこの理由で飛ばされる。
var skipReason string

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DB_DSN")

	if dsn == "" {
		// **データベースを用意した実行では飛ばさせない。**
		// 飛ばして緑になると、「実行された」と「実行できる状態にすら
		// ない」を区別できなくなる。
		//
		// 条件を CI かどうかにはしない。CI にはデータベースを
		// 用意しないジョブ（go test ./... を回すもの）もあり、
		// そちらまで失敗させることになる。**用意した側が名乗る。**
		if os.Getenv("TEST_DB_REQUIRED") != "" {
			fmt.Fprintln(os.Stderr,
				"TEST_DB_REQUIRED が立っているのに TEST_DB_DSN が無い。データベースを用意してから実行すること")
			os.Exit(1)
		}
		skipReason = "TEST_DB_DSN が未設定のため実行しない（実行方法は main_test.go の冒頭）"
		os.Exit(m.Run())
	}

	// **テスト用のデータベース以外には繋がせない。**
	// このテストは各ケースの前に全テーブルを空にする。
	// 開発用のデータベースを指したまま実行すると、手元のデータが消える。
	if !strings.Contains(dsn, "test") {
		fmt.Fprintln(os.Stderr,
			"TEST_DB_DSN のデータベース名に test を含めること（誤って開発用の DB を空にしないため）")
		os.Exit(1)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "接続の初期化に失敗した: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "データベースへ疎通できない: %v\n", err)
		os.Exit(1)
	}

	// マイグレーションが当たっていることを確認する。
	// 当たっていない DB を相手にすると、失敗の理由が
	// 「SQL が間違っている」なのか「表が無い」なのか分からなくなる。
	var prefectures int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM prefectures").Scan(&prefectures); err != nil {
		fmt.Fprintf(os.Stderr,
			"prefectures を読めない。マイグレーションを適用してから実行すること: %v\n", err)
		os.Exit(1)
	}
	if prefectures != 47 {
		fmt.Fprintf(os.Stderr, "prefectures が %d 件しかない。47 件必要である\n", prefectures)
		os.Exit(1)
	}

	testDB = db
	code := m.Run()
	_ = db.Close()
	os.Exit(code)
}

// 空にする表。**prefectures は含めない**（マスタであり、消すと投稿が作れない）。
//
// 並びは外部キーの向きに合わせてあるが、実行時は外部キー検査を
// 一時的に外すため、依存の順序で失敗することはない。
var truncateTargets = []string{
	"notifications",
	"comments",
	"likes",
	"follows",
	"post_tags",
	"tags",
	"media_variants",
	"media",
	"posts",
	"refresh_tokens",
	"users",
}

// newDB はテスト1件ぶんの接続を返し、**開始前にすべての表を空にする。**
//
// 後始末ではなく前始末にしているのは、失敗したテストが残したデータを
// そのまま調べられるようにするためである。
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	if skipReason != "" {
		t.Skip(skipReason)
	}

	ctx := t.Context()
	if _, err := testDB.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("外部キー検査を外せない: %v", err)
	}
	for _, table := range truncateTargets {
		if _, err := testDB.ExecContext(ctx, "TRUNCATE TABLE "+table); err != nil {
			t.Fatalf("%s を空にできない: %v", table, err)
		}
	}
	if _, err := testDB.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		t.Fatalf("外部キー検査を戻せない: %v", err)
	}
	return testDB
}

// ---------------------------------------------------------------------------
// 補助
// ---------------------------------------------------------------------------

// fakeSigner は署名付き URL の代わりに鍵をそのまま返す。
//
// **S3 はここでの検証対象ではない。** 画像の鍵が正しく引けているかだけを見る。
type fakeSigner struct{}

func (fakeSigner) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "signed:" + key, nil
}

var _ storage.URLSigner = fakeSigner{}

// createUser は利用者を1人作る。ハンドルは呼び出しごとに変える。
func createUser(t *testing.T, db *sql.DB, handle string) uint64 {
	t.Helper()
	id, err := NewAuthStore(db).CreateUser(
		t.Context(), handle, handle+"@example.test", "$2a$10$dummydummydummydummydu", handle,
	)
	if err != nil {
		t.Fatalf("利用者を作れない: %v", err)
	}
	return id.ID
}

// createProcessedMedia は「投稿に使える状態」の画像を1件作る。
//
// 実際には S3 のイベントで起動する処理が status を進めるが、
// ここでは対象外なので直接その状態にする。
func createProcessedMedia(t *testing.T, db *sql.DB, userID uint64, key string) uint64 {
	t.Helper()
	mediaID, err := NewPostStore(db).CreatePendingMedia(t.Context(), userID, key)
	if err != nil {
		t.Fatalf("画像を記録できない: %v", err)
	}
	_, err = db.ExecContext(t.Context(),
		"UPDATE media SET status = 'processed', mime = 'image/png', width = 10, height = 10, bytes = 100 WHERE id = ?",
		mediaID)
	if err != nil {
		t.Fatalf("画像の状態を進められない: %v", err)
	}
	return mediaID
}

// createPost は投稿を1件作る。画像は自動で1枚用意する。
func createPost(t *testing.T, db *sql.DB, userID uint64, body string, opts ...func(*CreatePostInput)) uint64 {
	t.Helper()
	mediaID := createProcessedMedia(t, db, userID, fmt.Sprintf("originals/%d/%s.png", userID, body))

	in := CreatePostInput{
		UserID:         userID,
		Body:           body,
		PrefectureCode: "01",
		Media:          []MediaAttachment{{MediaID: mediaID}},
	}
	for _, opt := range opts {
		opt(&in)
	}

	postID, err := NewPostStore(db).CreatePost(t.Context(), in)
	if err != nil {
		t.Fatalf("投稿を作れない: %v", err)
	}
	return postID
}

// countRows は表の件数を返す。where は省略できる。
func countRows(t *testing.T, db *sql.DB, table, where string, args ...any) int {
	t.Helper()
	query := "SELECT COUNT(*) FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	var n int
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s の件数を取得できない: %v", table, err)
	}
	return n
}
