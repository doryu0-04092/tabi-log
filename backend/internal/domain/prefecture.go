// Package domain はアプリケーションが扱う概念の型と規則を持つ。
//
// データベースにも HTTP にも依存しない。store 層はここへ変換して返し、
// httpapi 層はここから API の型へ変換する。両端の都合が中央へ染み出すのを防ぐ。
package domain

// Prefecture は都道府県を表す。
//
// 47件・不変のマスタであり、アプリケーションから追加・更新・削除しない。
type Prefecture struct {
	// Code は JIS X 0401 の都道府県コード。"01"〜"47" の2桁。
	//
	// 独自採番せず外部で定められたコードを使うのは、統計データや地図データと
	// 突き合わせるときに変換表を要さないためである。
	// **先頭のゼロに意味があるため文字列で扱う。**数値にすると "01" が 1 になる。
	Code string

	Name     string
	NameKana string

	// Region は八地方区分。「関東の投稿」のような粒度での絞り込みに使う。
	Region string
}
