// copyspec は API 仕様のファイルを1つ写す。
//
// **なぜ専用のコマンドがあるのか。**
// go:generate の行は OS のシェルを通らないため、cp や copy を
// 直接は書けない（Windows と Linux で書き分けにもなる）。
// 標準ライブラリだけで済む処理なので、小さなコマンドにしてある。
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	from := flag.String("from", "", "写す元のファイル")
	to := flag.String("to", "", "写す先のファイル")
	flag.Parse()

	if *from == "" || *to == "" {
		fmt.Fprintln(os.Stderr, "-from と -to の両方が要る")
		os.Exit(2)
	}

	data, err := os.ReadFile(*from)
	if err != nil {
		fmt.Fprintf(os.Stderr, "読めない: %v\n", err)
		os.Exit(1)
	}

	// **中身が同じなら書かない。** 書き込むと更新時刻が変わり、
	// 変更が無くてもビルドのキャッシュが効かなくなる。
	if existing, err := os.ReadFile(*to); err == nil && string(existing) == string(data) {
		return
	}

	if err := os.WriteFile(*to, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "書けない: %v\n", err)
		os.Exit(1)
	}
}
