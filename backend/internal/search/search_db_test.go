package search

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql" // database/sql に mysql ドライバを登録する
)

/*
検索を実際の MySQL に対して確かめる。

**ここは全文検索の索引（InnoDB FULLTEXT + ngram パーサ）に依存している。**
偽の実装では、検索式が意図どおりに解釈されているかを一切確認できない。

実際、**複数の語で検索すると何も返らない**不具合を、
単体テストではなく実環境で叩いて初めて見つけている。原因は入力全体を
1つのフレーズとして BOOLEAN MODE に渡していたことで、
「北海道 海鮮」が「北海道 海鮮」という連続した並びの検索になっていた。

実行方法は internal/store/main_test.go の冒頭と同じ（TEST_DB_DSN）。
*/

var testDB *sql.DB
var skipReason string

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		// 条件については internal/store/main_test.go の説明を参照。
		if os.Getenv("TEST_DB_REQUIRED") != "" {
			fmt.Fprintln(os.Stderr, "TEST_DB_REQUIRED が立っているのに TEST_DB_DSN が無い")
			os.Exit(1)
		}
		skipReason = "TEST_DB_DSN が未設定のため実行しない"
		os.Exit(m.Run())
	}
	if !strings.Contains(dsn, "test") {
		fmt.Fprintln(os.Stderr, "TEST_DB_DSN のデータベース名に test を含めること")
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

	testDB = db
	code := m.Run()
	_ = db.Close()
	os.Exit(code)
}

// setup は表を空にし、検索対象の投稿を入れて利用者 ID を返す。
func setup(t *testing.T, bodies map[string]string) (*sql.DB, map[string]uint64) {
	t.Helper()
	if skipReason != "" {
		t.Skip(skipReason)
	}
	ctx := t.Context()

	if _, err := testDB.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("外部キー検査を外せない: %v", err)
	}
	for _, table := range []string{"notifications", "comments", "likes", "follows", "post_tags", "tags", "media_variants", "media", "posts", "refresh_tokens", "users"} {
		if _, err := testDB.ExecContext(ctx, "TRUNCATE TABLE "+table); err != nil {
			t.Fatalf("%s を空にできない: %v", table, err)
		}
	}
	if _, err := testDB.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		t.Fatalf("外部キー検査を戻せない: %v", err)
	}

	res, err := testDB.ExecContext(ctx,
		"INSERT INTO users (handle, email, password_hash, display_name) VALUES ('searcher', 's@example.test', 'x', '探す人')")
	if err != nil {
		t.Fatalf("利用者を作れない: %v", err)
	}
	uid, _ := res.LastInsertId()

	ids := make(map[string]uint64, len(bodies))
	for label, body := range bodies {
		r, err := testDB.ExecContext(ctx,
			"INSERT INTO posts (user_id, body, prefecture_code, visited_on) VALUES (?, ?, '01', '2026-05-03')",
			uid, body)
		if err != nil {
			t.Fatalf("投稿を作れない: %v", err)
		}
		id, _ := r.LastInsertId()
		ids[label] = uint64(id)
	}
	return testDB, ids
}

const noCursorLimit = 20

func newCursor() Cursor { return Cursor{LikeCount: ^uint32(0), ID: 1 << 62} }

/*
複数の語での検索。

**入力全体を1つのフレーズとして渡すと、語の並びが一致する投稿しか
返らなくなる。** 実際にその状態になっており、
「北海道 海鮮」で1件も返らなかった。空白で区切られた語の
**すべてを含む**（AND）ものを返すのが期待する動きである。
*/
func Test複数の語はすべてを含むものを返す(t *testing.T) {
	db, ids := setup(t, map[string]string{
		"両方":   "北海道で海鮮丼を食べた",
		"片方だけ": "北海道でラーメンを食べた",
		"別の片方": "静岡で海鮮丼を食べた",
	})

	searcher := NewMySQLSearcher(db)
	got, _, err := searcher.SearchPosts(t.Context(),
		Filters{Keyword: "北海道 海鮮", Sort: SortLatest}, newCursor(), noCursorLimit)
	if err != nil {
		t.Fatalf("検索に失敗した: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("%d件返った。両方を含む1件だけのはず: %v", len(got), got)
	}
	if got[0] != ids["両方"] {
		t.Errorf("投稿 %d が返った。%d のはず", got[0], ids["両方"])
	}
}

func Test語の順序を入れ替えても同じものが返る(t *testing.T) {
	db, ids := setup(t, map[string]string{
		"対象": "京都の紅葉を見に行った",
	})

	searcher := NewMySQLSearcher(db)
	for _, keyword := range []string{"京都 紅葉", "紅葉 京都"} {
		got, _, err := searcher.SearchPosts(t.Context(),
			Filters{Keyword: keyword, Sort: SortLatest}, newCursor(), noCursorLimit)
		if err != nil {
			t.Fatalf("%q の検索に失敗した: %v", keyword, err)
		}
		if len(got) != 1 || got[0] != ids["対象"] {
			t.Errorf("%q で %v が返った。[%d] のはず", keyword, got, ids["対象"])
		}
	}
}

/*
検索の入力は**必ずプレースホルダを通す。**

検索だけは条件が可変のため、sqlc の外で SQL を組み立てている。
組み立てる対象は「条件の形」だけで、**値は一切連結しない**という
作りになっているかを、実際に壊れる形の入力で確かめる。
*/
func Test記号を含む入力でも壊れない(t *testing.T) {
	db, _ := setup(t, map[string]string{
		"対象": "普通の投稿",
	})

	searcher := NewMySQLSearcher(db)
	inputs := []string{
		"' OR 1=1 --",
		`" OR ""="`,
		"'; DROP TABLE posts; --",
		"100% 割引",
		`\ バックスラッシュ`,
		"アンダー_スコア",
	}

	for _, keyword := range inputs {
		if !ValidKeyword(keyword) {
			continue
		}
		if _, _, err := searcher.SearchPosts(t.Context(),
			Filters{Keyword: keyword, Sort: SortLatest}, newCursor(), noCursorLimit); err != nil {
			t.Errorf("%q で失敗した: %v", keyword, err)
		}
	}

	// **表が消えていないこと。** 連結していれば DROP が通り得る。
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
		t.Fatalf("posts を読めない。表が消えている可能性がある: %v", err)
	}
	if n != 1 {
		t.Errorf("投稿が %d件。1件のはず", n)
	}
}

/*
利用者の検索は前方一致・部分一致で行う。

**LIKE の特殊文字を素通しすると、`%` だけで全員が返る。**
入力をそのまま条件にしていないことを確かめる。
*/
func Test利用者検索でLIKEの記号が効かない(t *testing.T) {
	db, _ := setup(t, nil)
	if _, err := db.ExecContext(t.Context(),
		"INSERT INTO users (handle, email, password_hash, display_name) VALUES ('another', 'a@example.test', 'x', '別の人')"); err != nil {
		t.Fatalf("利用者を追加できない: %v", err)
	}

	searcher := NewMySQLSearcher(db)
	got, _, err := searcher.SearchUsers(t.Context(), "%", 0, noCursorLimit)
	if err != nil {
		t.Fatalf("利用者検索に失敗した: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("%% で %d人が返った。0人のはず", len(got))
	}
}

func Test都道府県と語の両方で絞り込める(t *testing.T) {
	db, ids := setup(t, map[string]string{
		"北海道の海鮮": "海鮮丼がおいしかった",
	})
	// 別の都道府県の投稿を足す。
	if _, err := db.ExecContext(t.Context(),
		"INSERT INTO posts (user_id, body, prefecture_code, visited_on) SELECT user_id, '海鮮丼がおいしかった', '13', '2026-05-03' FROM posts LIMIT 1"); err != nil {
		t.Fatalf("投稿を追加できない: %v", err)
	}

	searcher := NewMySQLSearcher(db)
	got, _, err := searcher.SearchPosts(t.Context(),
		Filters{Keyword: "海鮮", PrefectureCode: "01", Sort: SortLatest}, newCursor(), noCursorLimit)
	if err != nil {
		t.Fatalf("検索に失敗した: %v", err)
	}
	if len(got) != 1 || got[0] != ids["北海道の海鮮"] {
		t.Errorf("%v が返った。[%d] のはず", got, ids["北海道の海鮮"])
	}
}
