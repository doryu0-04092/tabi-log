package store

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"
)

/*
問い合わせの実行計画を確かめる。

**この検証は実行しないと成立しない。** 索引を張ったこと自体はスキーマを
読めば分かるが、**その索引で ORDER BY まで解決できているか**は
EXPLAIN を見るしかない。

実際に4回、索引はあるのに並べ替えが発生している状態を作っている
（新着 / 利用者ごと / 旅行履歴 / 都道府県ごと）。原因は毎回同じで、
**「絞り込みに使える索引」と「並び順を解決できる索引」を同じものだと
思い込んだこと**である。最後の1件はこのテストを書いていて見つかった。

**SQL は sqlc の入力ファイルから読む。** テストに書き写すと、
本物の SQL を変えたときにテストだけ古いまま通ってしまう。

---

**件数を実態に寄せる必要がある。** 数百件では全表走査のほうが安いと
判断され、EXPLAIN の結果が索引の良し悪しを表さなくなる。
利用者40人・投稿2万件を入れてから見る（要件の想定は利用者2000人・
投稿20万件なので、その1/10の比率にあたる）。
*/

// readNamedQuery は sqlc の入力ファイルから名前付きの問い合わせを取り出す。
//
// sqlc.arg() / sqlc.narg() は素の MySQL では解釈できないため ? に戻す。
func readNamedQuery(t *testing.T, file, name string) string {
	t.Helper()

	raw, err := os.ReadFile("../../db/queries/" + file)
	if err != nil {
		t.Fatalf("問い合わせファイルを読めない: %v", err)
	}

	marker := "-- name: " + name + " "
	start := strings.Index(string(raw), marker)
	if start < 0 {
		t.Fatalf("%s に %s が見つからない", file, name)
	}
	// マーカー行の次から、次の -- name: まで。
	body := string(raw)[start:]
	if nl := strings.Index(body, "\n"); nl >= 0 {
		body = body[nl+1:]
	}
	if next := strings.Index(body, "-- name:"); next >= 0 {
		body = body[:next]
	}

	// コメント行を落とす。次の問い合わせの説明が末尾に混ざるため。
	lines := make([]string, 0, 32)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		lines = append(lines, line)
	}

	query := strings.Join(lines, "\n")
	query = regexp.MustCompile(`sqlc\.n?arg\('[a-z_]+'\)`).ReplaceAllString(query, "?")
	return strings.TrimSpace(query)
}

// explainExtra は EXPLAIN の Extra 列をすべて連結して返す。
func explainExtra(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN を実行できない: %v\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("EXPLAIN の列を取得できない: %v", err)
	}

	var extras []string
	for rows.Next() {
		cells := make([]any, len(columns))
		for i := range cells {
			cells[i] = new(sql.NullString)
		}
		if err := rows.Scan(cells...); err != nil {
			t.Fatalf("EXPLAIN の行を読めない: %v", err)
		}
		for i, name := range columns {
			if name == "Extra" {
				extras = append(extras, cells[i].(*sql.NullString).String)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN の読み出しに失敗した: %v", err)
	}
	return strings.Join(extras, " | ")
}

// explainColumn は EXPLAIN の指定した列をすべて連結して返す。
//
// **Extra だけでは足りない。** 並べ替えが出ていなくても、
// 意図した索引ではなく PRIMARY を範囲走査していることがある
// （コメントの一覧が実際にそうだった）。どの索引が選ばれたかを見る。
func explainColumn(t *testing.T, db *sql.DB, column, query string, args ...any) string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN を実行できない: %v\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("EXPLAIN の列を取得できない: %v", err)
	}

	var values []string
	for rows.Next() {
		cells := make([]any, len(columns))
		for i := range cells {
			cells[i] = new(sql.NullString)
		}
		if err := rows.Scan(cells...); err != nil {
			t.Fatalf("EXPLAIN の行を読めない: %v", err)
		}
		for i, name := range columns {
			if name == column {
				values = append(values, cells[i].(*sql.NullString).String)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN の読み出しに失敗した: %v", err)
	}
	return strings.Join(values, " | ")
}

// 0〜9 を並べるための副問い合わせ。数表を作らずに件数を掛け合わせる。
const digits = `(SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
  UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)`

// seedForPlan は実行計画を見るための投稿を入れ、対象の利用者 ID を返す。
//
// **投稿の作り方はここでの検証対象ではない**ため直接入れる。
// 都道府県と日付をばらすのは、統計が偏ると索引の選ばれ方が変わるためである。
func seedForPlan(t *testing.T, db *sql.DB) uint64 {
	t.Helper()
	ctx := t.Context()

	// 利用者40人。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (handle, email, password_hash, display_name)
		SELECT CONCAT('plan', t.n), CONCAT('plan', t.n, '@example.test'), 'x', CONCAT('plan', t.n)
		FROM (SELECT a.n + b.n * 10 AS n FROM `+digits+` a, `+digits+` b WHERE a.n + b.n * 10 < 40) t
	`); err != nil {
		t.Fatalf("利用者をまとめて入れられない: %v", err)
	}

	// 1人あたり500件、合計2万件。都道府県・訪問日・投稿日をばらす。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO posts (user_id, body, prefecture_code, visited_on, created_at)
		SELECT u.id,
		       CONCAT('計画確認 ', s.n),
		       LPAD((s.n % 47) + 1, 2, '0'),
		       DATE_SUB(CURRENT_DATE, INTERVAL s.n DAY),
		       DATE_SUB(NOW(), INTERVAL (u.id * 500 + s.n) MINUTE)
		FROM users u
		JOIN (SELECT a.n + b.n * 10 + c.n * 100 AS n FROM `+digits+` a, `+digits+` b, `+digits+` c
		      WHERE a.n + b.n * 10 + c.n * 100 < 500) s
	`); err != nil {
		t.Fatalf("投稿をまとめて入れられない: %v", err)
	}

	// **統計が古いままだと EXPLAIN が実態とずれる。**
	if _, err := db.ExecContext(ctx, "ANALYZE TABLE posts, users"); err != nil {
		t.Fatalf("統計を更新できない: %v", err)
	}

	var userID uint64
	if err := db.QueryRowContext(ctx, "SELECT id FROM users ORDER BY id LIMIT 1").Scan(&userID); err != nil {
		t.Fatalf("対象の利用者を取れない: %v", err)
	}

	var otherID uint64
	if err := db.QueryRowContext(ctx,
		"SELECT id FROM users WHERE id <> ? ORDER BY id LIMIT 1", userID).Scan(&otherID); err != nil {
		t.Fatalf("もう1人の利用者を取れない: %v", err)
	}

	// 通知も同じだけ入れる。**空の表に EXPLAIN をかけても意味が無い。**
	// 自分あての通知は作れない制約があるので、宛先と行為者をずらす。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO notifications (user_id, actor_id, type)
		SELECT p.user_id, IF(p.user_id = ?, ?, ?), 'like' FROM posts p LIMIT 20000
	`, userID, otherID, userID); err != nil {
		t.Fatalf("通知をまとめて入れられない: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE TABLE notifications"); err != nil {
		t.Fatalf("統計を更新できない: %v", err)
	}

	return userID
}

func TestカーソルでたどるSELECTが並べ替えを起こさない(t *testing.T) {
	db := newDB(t)
	userID := seedForPlan(t, db)

	const maxCursor = uint64(1) << 62

	cases := []struct {
		name  string
		file  string
		query string
		args  []any
	}{
		{
			name:  "新着フィード",
			file:  "posts.sql",
			query: "ListPostsBefore",
			args:  []any{maxCursor, 21},
		},
		{
			// **索引を FORCE INDEX で固定してある問い合わせ。**
			// posts には user_id で始まる索引が複数あり、指定しないと
			// 並べ替えを解決できない側が選ばれる（実測済み）。
			name:  "利用者ごとの投稿",
			file:  "posts.sql",
			query: "ListPostsByUserBefore",
			args:  []any{userID, maxCursor, 21},
		},
		{
			// 訪問日と ID の2段のカーソル。索引も2段になっている。
			name:  "旅行履歴",
			file:  "posts.sql",
			query: "ListTravelsByUserBefore",
			args:  []any{userID, "9999-12-31", "9999-12-31", maxCursor, 21},
		},
		{
			name:  "通知の一覧",
			file:  "notifications.sql",
			query: "ListNotificationsBefore",
			args:  []any{userID, maxCursor, 21},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := readNamedQuery(t, tc.file, tc.query)
			extra := explainExtra(t, db, query, tc.args...)
			if strings.Contains(extra, "Using filesort") {
				t.Errorf("%s で並べ替えが発生している。索引の列の並びが ORDER BY と合っていない\nExtra: %s\n%s",
					tc.query, extra, query)
			}
		})
	}
}

/*
コメントの一覧。**索引 ix_comments_post は (post_id, created_at, id) である。**

post_id は等価で絞れるが、その中の並びは created_at → id になる。
ORDER BY c.id を索引だけで解決できるのは、created_at の順と id の順が
一致すると分かっている場合だけであり、**データベースはそれを知らない。**

監査（2026-08-31、M10）で疑いとして挙げ、根拠を出さずに残していた。
**EXPLAIN で事実にする。**

件数と**分布**を実態に寄せる必要がある。数十件では全表走査のほうが安いと
判断され、結果が索引の良し悪しを表さない。1つの投稿に偏らせるのも同じで、
PRIMARY を辿るだけで LIMIT が埋まってしまう。
400 件の投稿に 25 件ずつ、id を入り混じらせて入れる。
*/
func Testコメントの一覧が並べ替えを起こさない(t *testing.T) {
	db := newDB(t)
	userID := seedForPlan(t, db)
	ctx := t.Context()

	var postID uint64
	if err := db.QueryRowContext(ctx, "SELECT id FROM posts ORDER BY id LIMIT 1").Scan(&postID); err != nil {
		t.Fatalf("対象の投稿を取れない: %v", err)
	}

	// **分布を実態に寄せる。** 1つの投稿に偏らせると、その投稿の
	// コメントが全体の何割も占めることになり、PRIMARY を id 順に辿っても
	// LIMIT 21 がすぐ埋まる。それでは索引の要否を判定できない
	// （実際に 1万件中 2000 件を偏らせて測り、判定にならなかった）。
	//
	// 実際には「1つの投稿のコメントは全体のごく一部」である。
	// 400 件の投稿に 25 件ずつ、id が入り混じるように入れる。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO comments (post_id, user_id, body)
		SELECT t.post_id, ?, CONCAT('計画確認 ', t.n)
		FROM (
		  SELECT p.id AS post_id, s.n AS n
		  FROM (SELECT id FROM posts ORDER BY id LIMIT 400) p
		  JOIN (SELECT a.n + b.n * 10 AS n FROM `+digits+` a, `+digits+` b WHERE a.n + b.n * 10 < 25) s
		  ORDER BY s.n, p.id
		) t
	`, userID); err != nil {
		t.Fatalf("コメントをまとめて入れられない: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE TABLE comments"); err != nil {
		t.Fatalf("統計を更新できない: %v", err)
	}

	query := readNamedQuery(t, "comments.sql", "ListCommentsAfter")

	extra := explainExtra(t, db, query, postID, uint64(0), 21)
	if strings.Contains(extra, "Using filesort") {
		t.Errorf("コメントの一覧で並べ替えが発生している\nExtra: %s\n%s", extra, query)
	}

	// **並べ替えが出ないことだけでは足りない。**
	// (post_id, created_at, id) だった頃は、並べ替えは出ないが
	// PRIMARY を id > ? で範囲走査し、post_id を Using where で捨てていた
	// （実測 rows=4957 / filtered=20.17%）。他の投稿のコメントまで辿るため、
	// 総量に比例して悪化する。**選ばれた索引まで見る。**
	key := explainColumn(t, db, "key", query, postID, uint64(0), 21)
	if !strings.Contains(key, "ix_comments_post") {
		t.Errorf("コメントの索引が使われていない。選ばれた索引: %s\n%s", key, query)
	}
}

/*
都道府県ごとの新着。**検索は sqlc の外で組み立てている**ため、
問い合わせファイルから読めない。ここだけは形を書く。

**この形が壊れていたことをこのテストで見つけた。**
ix_posts_prefecture_created (prefecture_code, created_at DESC) が
ix_posts_prefecture_id (prefecture_code, id DESC) より先に選ばれ、
並べ替えが発生していた（000007 で置き換えた）。
*/
func Test都道府県で絞った一覧が並べ替えを起こさない(t *testing.T) {
	db := newDB(t)
	seedForPlan(t, db)

	const query = `SELECT p.id, p.like_count FROM posts p
		WHERE p.prefecture_code = ? AND p.id < ?
		ORDER BY p.id DESC LIMIT ?`

	extra := explainExtra(t, db, query, "13", uint64(1)<<62, 21)
	if strings.Contains(extra, "Using filesort") {
		t.Errorf("都道府県で絞った一覧で並べ替えが発生している\nExtra: %s", extra)
	}
}

// **フォロー中フィードだけは並べ替えが起きる。** 複数の利用者の投稿を
// 1つの順序に混ぜる以上、索引1本では解決できないためである。
//
// 消せない代わりに、**消せないことを記録しておく。** ここが静かに変わった
// （＝作り方を変えた）ときに、説明との食い違いに気づけるようにする。
func Testフォロー中フィードは並べ替えが起きる(t *testing.T) {
	db := newDB(t)
	userID := seedForPlan(t, db)

	var followee uint64
	if err := db.QueryRowContext(t.Context(),
		"SELECT id FROM users WHERE id <> ? ORDER BY id LIMIT 1", userID).Scan(&followee); err != nil {
		t.Fatalf("フォロー先を取れない: %v", err)
	}
	if err := NewFollowStore(db).Follow(t.Context(), userID, followee); err != nil {
		t.Fatalf("フォローできない: %v", err)
	}

	query := readNamedQuery(t, "posts.sql", "ListFollowingFeedBefore")
	extra := explainExtra(t, db, query, userID, uint64(1)<<62, 21)

	if !strings.Contains(extra, "filesort") {
		t.Errorf("並べ替えが出なくなっている。作り方を変えたなら db/queries/posts.sql の説明も直すこと\nExtra: %s", extra)
	}
}
