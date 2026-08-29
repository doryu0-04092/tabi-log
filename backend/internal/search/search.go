// Package search は投稿と利用者の検索を担う。
//
// **ここだけ sqlc を使わず database/sql で SQL を組み立てる。**
// sqlc は静的な SQL からコードを生成する仕組みであり、絞り込みの軸が
// 指定されたぶんだけ AND が増減するクエリは扱えない。
//
// 手で組み立てる以上、**値は必ずプレースホルダにバインドする。**
// ユーザー入力を SQL 文字列に連結しない。この方針を破らないよう、
// 文字列連結で組み立ててよいのは「こちらが書いた固定の断片」だけに
// 限っている（列名・演算子・ORDER BY）。
package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
)

// MinKeywordRunes は検索できるキーワードの最短の長さ。
//
// **ngram パーサのトークン長が2であるため、1文字では何にもヒットしない。**
// 黙って空の結果を返すと「壊れている」と受け取られるので、
// 呼び出し側で弾いて理由を伝える。
const MinKeywordRunes = 2

// SortOrder は並び順。
type SortOrder string

const (
	SortLatest  SortOrder = "latest"
	SortPopular SortOrder = "popular"
)

// Filters は絞り込みの軸。
//
// **軸は互いに直交している。** 指定されたものだけが AND で足される。
// 軸を増やすときは、ここに項目を足して buildConditions に1つ条件を足す。
type Filters struct {
	Keyword        string
	PrefectureCode string
	Region         string
	Tag            string
	Handle         string
	VisitedFrom    *time.Time
	VisitedTo      *time.Time
	Since          *time.Time
	Sort           SortOrder
}

// Cursor は続きを取る位置。
//
// **並び順によって意味が変わる。** 新着では ID だけを使い、
// 人気順では (LikeCount, ID) の組で「同じいいね数の中の位置」まで示す。
type Cursor struct {
	LikeCount uint32
	ID        uint64
}

// Searcher は検索の入口。
//
// 実装を差し替えられるようにインターフェースを切っている。
// ngram の全文索引はトークン長2で全文を刻むため索引が肥大しやすく、
// 実測次第で OpenSearch へ移す判断があり得る（tech-stack.md 参照）。
type Searcher interface {
	SearchPosts(ctx context.Context, f Filters, cursor Cursor, limit int) ([]uint64, Cursor, error)
	SearchUsers(ctx context.Context, keyword string, cursorID uint64, limit int) ([]domain.User, uint64, error)
}

// MySQLSearcher は MySQL の InnoDB FULLTEXT（ngram）を使う実装。
type MySQLSearcher struct {
	db *sql.DB
	// logger は組み立てた条件を残す先。nil でもよい。
	logger *slog.Logger
}

func NewMySQLSearcher(db *sql.DB) *MySQLSearcher {
	return &MySQLSearcher{db: db}
}

// WithLogger は記録先を差し込む。
//
// **記録するのは「どう組み立てたか」と「何件返したか」であって、
// 利用者が入れた語そのものではない。** 検索語は利用者が何を探したかを
// そのまま表すため、ログに残す情報ではない。
func (s *MySQLSearcher) WithLogger(logger *slog.Logger) *MySQLSearcher {
	s.logger = logger
	return s
}

// SearchPosts は条件に合う投稿の ID を並び順どおりに返す。
//
// **本体は返さない。** 投稿の組み立て（画像・タグ・いいねの状態）は
// フィードと同じ手順で行う必要があり、それを2か所に持ちたくない。
// ここは「どの投稿がどの順で並ぶか」だけを決める。
func (s *MySQLSearcher) SearchPosts(ctx context.Context, f Filters, cursor Cursor, limit int) ([]uint64, Cursor, error) {
	conds, args := buildConditions(f)

	// 並び順とカーソルは対で決まる。片方だけ変えると、
	// 続きが飛んだり重複したりする。
	var order string
	switch f.Sort {
	case SortPopular:
		// (like_count, id) の降順。索引 ix_posts_like_count と同じ並び。
		conds = append(conds, "(p.like_count < ? OR (p.like_count = ? AND p.id < ?))")
		args = append(args, cursor.LikeCount, cursor.LikeCount, cursor.ID)
		order = "ORDER BY p.like_count DESC, p.id DESC"
	default:
		conds = append(conds, "p.id < ?")
		args = append(args, cursor.ID)
		order = "ORDER BY p.id DESC"
	}

	// タグと投稿者はそれぞれ結合が要る。指定されたときだけ足す。
	joins := ""
	if f.Tag != "" {
		joins += " JOIN post_tags pt ON pt.post_id = p.id JOIN tags t ON t.id = pt.tag_id"
	}
	if f.Handle != "" {
		joins += " JOIN users au ON au.id = p.user_id"
	}
	if f.Region != "" {
		joins += " JOIN prefectures pref ON pref.code = p.prefecture_code"
	}

	// **組み立てているのは、こちらが書いた固定の断片だけである。**
	// 利用者が入れた値は1つ残らず args に入り、プレースホルダで渡る。
	query := fmt.Sprintf(
		"SELECT p.id, p.like_count FROM posts p%s WHERE %s %s LIMIT ?",
		joins, strings.Join(conds, " AND "), order,
	)
	// 1件多く取って「続きがあるか」を判定する。
	args = append(args, limit+1)

	// **0件だったときに、条件の組み立てを疑えるようにする。**
	// 実際に「複数の語で検索すると必ず0件」という不具合を出しており、
	// そのときは「検索できない」という報告からは何も分からなかった。
	// 語そのものは残さず、語数と条件の数だけを残す。
	if s.logger != nil {
		s.logger.DebugContext(ctx, "投稿を検索する",
			slog.Int("keyword_terms", len(strings.Fields(f.Keyword))),
			slog.Int("conditions", len(conds)),
			slog.String("sort", string(f.Sort)),
			slog.Bool("has_prefecture", f.PrefectureCode != ""),
			slog.Bool("has_tag", f.Tag != ""),
			slog.Bool("has_handle", f.Handle != ""),
		)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, Cursor{}, fmt.Errorf("投稿の検索に失敗した: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type hit struct {
		id        uint64
		likeCount uint32
	}
	hits := make([]hit, 0, limit+1)
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.likeCount); err != nil {
			return nil, Cursor{}, fmt.Errorf("検索結果を読み取れない: %w", err)
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, Cursor{}, fmt.Errorf("検索結果の読み取りが中断した: %w", err)
	}

	hasMore := len(hits) > limit
	if hasMore {
		hits = hits[:limit]
	}

	ids := make([]uint64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.id)
	}

	var next Cursor
	if hasMore && len(hits) > 0 {
		last := hits[len(hits)-1]
		next = Cursor{LikeCount: last.likeCount, ID: last.id}
	}
	return ids, next, nil
}

// buildConditions は指定された軸だけを条件に変える。
//
// 返す条件はすべて「固定の文字列」であり、値は args 側にしか入らない。
func buildConditions(f Filters) (conds []string, args []any) {
	if f.Keyword != "" {
		// **BOOLEAN MODE で「すべての語を含む」として渡す。**
		// 自然文モードは「どれかを含む」になり、語を足すほど結果が広がる。
		// 探すときに語を足すのは絞り込むためなので、意図と逆になる。
		conds = append(conds, "MATCH(p.body, p.spot_name) AGAINST(? IN BOOLEAN MODE)")
		args = append(args, booleanQuery(f.Keyword))
	}
	if f.PrefectureCode != "" {
		conds = append(conds, "p.prefecture_code = ?")
		args = append(args, f.PrefectureCode)
	}
	if f.Region != "" {
		conds = append(conds, "pref.region = ?")
		args = append(args, f.Region)
	}
	if f.Tag != "" {
		conds = append(conds, "t.name = ?")
		args = append(args, f.Tag)
	}
	if f.Handle != "" {
		conds = append(conds, "au.handle = ?")
		args = append(args, f.Handle)
	}
	if f.VisitedFrom != nil {
		conds = append(conds, "p.visited_on >= ?")
		args = append(args, *f.VisitedFrom)
	}
	if f.VisitedTo != nil {
		conds = append(conds, "p.visited_on <= ?")
		args = append(args, *f.VisitedTo)
	}
	if f.Since != nil {
		conds = append(conds, "p.created_at >= ?")
		args = append(args, *f.Since)
	}
	// 条件が1つも無くても WHERE を書けるようにしておく。
	// カーソルの条件が必ず足されるため、実際には空にならない。
	return conds, args
}

// booleanQuery は入力を BOOLEAN MODE の検索式に変える。
//
// **空白で区切られた語のすべてを含むものを探す**（AND）。
// 「海鮮丼 函館」で、両方を含む投稿だけが出る。自然文モードの
// 「どれかを含む」だと、語を足すほど結果が広がって絞り込めない。
//
// 各語は二重引用符で囲む。**BOOLEAN MODE では + - > < ( ) ~ * " が
// 演算子として働く**ため、囲まないと利用者が入れた記号が演算子として
// 解釈される（"-東京" が「東京を含まない」になる等）。
//
// **トークン長に満たない語は落とす。** ngram の索引はトークン長2で
// 刻まれており、1文字の語を AND に混ぜると必ず0件になる。
//
// 値はプレースホルダで渡るため、これは SQL インジェクション対策ではなく
// **検索の意味を安定させるための処理**である。
func booleanQuery(keyword string) string {
	// 二重引用符は語の囲いを途中で閉じてしまうため取り除く。
	cleaned := strings.ReplaceAll(keyword, `"`, " ")

	terms := make([]string, 0, 4)
	for _, w := range strings.Fields(cleaned) {
		if utf8.RuneCountInString(w) < MinKeywordRunes {
			continue
		}
		terms = append(terms, `+"`+w+`"`)
	}
	return strings.Join(terms, " ")
}

// SearchUsers はハンドルと表示名を対象に部分一致で探す。
//
// **前方一致ではなく部分一致のため索引が効かない。** 想定利用者数は
// 2000人であり全件走査でも問題にならない規模である。桁が変わるなら、
// 検索そのものを別の仕組みへ移す判断になる。
func (s *MySQLSearcher) SearchUsers(ctx context.Context, keyword string, cursorID uint64, limit int) ([]domain.User, uint64, error) {
	const query = `
SELECT id, handle, display_name, bio
FROM users
WHERE deleted_at IS NULL
  AND id > ?
  AND (handle LIKE ? OR display_name LIKE ?)
ORDER BY id
LIMIT ?`

	like := "%" + escapeLike(keyword) + "%"
	rows, err := s.db.QueryContext(ctx, query, cursorID, like, like, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("利用者の検索に失敗した: %w", err)
	}
	defer func() { _ = rows.Close() }()

	users := make([]domain.User, 0, limit+1)
	for rows.Next() {
		var u domain.User
		var bio sql.NullString
		if err := rows.Scan(&u.ID, &u.Handle, &u.DisplayName, &bio); err != nil {
			return nil, 0, fmt.Errorf("検索結果を読み取れない: %w", err)
		}
		if bio.Valid {
			v := bio.String
			u.Bio = &v
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("検索結果の読み取りが中断した: %w", err)
	}

	if len(users) <= limit {
		return users, 0, nil
	}
	users = users[:limit]
	return users, users[len(users)-1].ID, nil
}

// escapeLike は LIKE のワイルドカードを打ち消す。
//
// **% と _ は LIKE のパターン記号である。** 打ち消さないと
// 「%」で検索した利用者に全件が返る。値はプレースホルダで渡るため、
// これも SQL インジェクション対策ではなく検索の意味の話である。
//
// 打ち消しに使う \ は MySQL の LIKE の既定のエスケープ文字である。
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ValidKeyword は検索に使える語が1つ以上あるかを返す。
//
// **短すぎる語しか無い入力を通すと、必ず0件になる。**
// 「探したのに何も出ない」より「短すぎる」と伝えるほうが親切である。
func ValidKeyword(keyword string) bool {
	for _, w := range strings.Fields(keyword) {
		if utf8.RuneCountInString(w) >= MinKeywordRunes {
			return true
		}
	}
	return false
}
