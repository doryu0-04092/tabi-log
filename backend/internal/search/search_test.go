package search

import (
	"strings"
	"testing"
	"time"
)

// **ここは唯一 SQL を手で組み立てる場所である。**
// 「値がプレースホルダに乗っているか」を型検査では確かめられないため、
// テストで押さえる。

func TestBuildConditionsBindsEveryValue(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	since := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	f := Filters{
		Keyword:        "函館",
		PrefectureCode: "01",
		Region:         "北海道",
		Tag:            "海鮮",
		Handle:         "traveler",
		VisitedFrom:    &from,
		VisitedTo:      &to,
		Since:          &since,
	}

	conds, args := buildConditions(f)

	// 8つの軸すべてが条件になっていること。
	if len(conds) != 8 {
		t.Fatalf("条件が %d 個。8個を期待した: %v", len(conds), conds)
	}
	// **条件の数とプレースホルダの数が一致すること。**
	// ずれていれば、値が SQL 文へ直接埋め込まれた疑いがある。
	placeholders := 0
	for _, c := range conds {
		placeholders += strings.Count(c, "?")
	}
	if placeholders != len(args) {
		t.Fatalf("プレースホルダ %d 個に対し値 %d 個。数が合わない", placeholders, len(args))
	}

	// 条件の文字列に利用者の入力が混ざっていないこと。
	joined := strings.Join(conds, " ")
	for _, v := range []string{"函館", "01", "北海道", "海鮮", "traveler"} {
		if strings.Contains(joined, v) {
			t.Fatalf("条件に入力値が埋め込まれている: %q が %q に含まれる", v, joined)
		}
	}
}

func TestBuildConditionsOnlyIncludesGivenAxes(t *testing.T) {
	conds, args := buildConditions(Filters{PrefectureCode: "13"})

	if len(conds) != 1 || len(args) != 1 {
		t.Fatalf("指定していない軸まで条件になっている: %v", conds)
	}
	if !strings.Contains(conds[0], "prefecture_code") {
		t.Fatalf("期待した条件でない: %v", conds)
	}
	if args[0] != "13" {
		t.Fatalf("値が %v。\"13\" を期待した", args[0])
	}
}

func TestBuildConditionsEmpty(t *testing.T) {
	conds, args := buildConditions(Filters{})
	if len(conds) != 0 || len(args) != 0 {
		t.Fatalf("何も指定していないのに条件がある: %v", conds)
	}
}

// **語を足すほど絞り込まれること（AND）。**
// 自然文モードの「どれかを含む」だと、語を足すほど結果が広がって
// 絞り込めない。探すときに語を足すのは絞るためである。
func TestBooleanQueryRequiresEveryTerm(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"1語", "函館", `+"函館"`},
		{"2語はどちらも必須", "海鮮丼 函館", `+"海鮮丼" +"函館"`},
		// **記号を演算子として解釈しない。**
		// "-東京" が「東京を含まない」に化けると意図と逆になる。
		{"先頭のハイフン", "-東京", `+"-東京"`},
		// 記号も1文字として数えるため "+海" は2文字扱いで残る。
		// ngram の索引に当たるのは「海」だけであり実際には拾えないが、
		// **記号を数から外すと今度は本当に短い語を通してしまう。**
		// 単純な数え方のまま、演算子として解釈させないことを優先する。
		{"演算子まじり", "+海 -山道", `+"+海" +"-山道"`},
		{"二重引用符は落とす", `"引用符"`, `+"引用符"`},
		// トークン長に満たない語は落とす。混ぜると必ず0件になる。
		{"短い語は落とす", "海 海鮮丼", `+"海鮮丼"`},
		{"短い語だけなら空", "海 山", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := booleanQuery(tt.in)
			if got != tt.want {
				t.Errorf("booleanQuery(%q) = %q。%q を期待した", tt.in, got, tt.want)
			}
			// 二重引用符は必ず対で閉じていること。
			// 閉じないと以降が演算子として解釈される。
			if strings.Count(got, `"`)%2 != 0 {
				t.Errorf("booleanQuery(%q) = %q。二重引用符が閉じていない", tt.in, got)
			}
		})
	}
}

// **LIKE のワイルドカードを打ち消す。**
// 打ち消さないと「%」で検索した利用者に全件が返る。
func TestEscapeLike(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"たびびと", "たびびと"},
		{"100%", `100\%`},
		{"a_b", `a\_b`},
		{`back\slash`, `back\\slash`},
	}

	for _, tt := range tests {
		if got := escapeLike(tt.in); got != tt.want {
			t.Errorf("escapeLike(%q) = %q。%q を期待した", tt.in, got, tt.want)
		}
	}
}

// ngram のトークン長は2。1文字では索引に当たらない。
// 使える語が1つでもあれば検索できる。
func TestValidKeyword(t *testing.T) {
	tests := map[string]bool{
		"":       false,
		" ":      false,
		"あ":      false,
		"あい":     true,
		"  あい  ": true,
		"ab":     true,
		"a":      false,
		"あ い":    false,
		"あ いろ":   true,
	}

	for in, want := range tests {
		if got := ValidKeyword(in); got != want {
			t.Errorf("ValidKeyword(%q) = %v。%v を期待した", in, got, want)
		}
	}
}
