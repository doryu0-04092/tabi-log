// Command lambdapack は実行ファイルを Lambda 用の zip にまとめる。
//
// **`zip` コマンドに頼らないための道具である。** 開発環境に zip が
// 入っているとは限らず（Windows の Git Bash には unzip しか無い）、
// ビルド手順が環境によって通ったり通らなかったりするのを避ける。
//
// もう1つの理由が実行権限である。Lambda の provided.al2023 ランタイムは
// **zip 内の bootstrap に実行権限が付いていることを要求する**。
// Windows のアーカイブ機能では Unix の権限を付けられず、
// 「zip はできるが Lambda が起動しない」という分かりにくい失敗になる。
//
//	使い方: go run ./cmd/lambdapack <入力ファイル> <出力zip>
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "使い方: lambdapack <入力ファイル> <出力zip>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "失敗した: %v\n", err)
		os.Exit(1)
	}
}

func run(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)

	header := &zip.FileHeader{
		Name:   filepath.Base(src),
		Method: zip.Deflate,
	}
	// **実行権限を明示的に付ける。** これが無いと Lambda は起動時に
	// bootstrap を実行できず、原因の分かりにくい失敗になる。
	header.SetMode(0o755)

	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}
	return out.Close()
}
