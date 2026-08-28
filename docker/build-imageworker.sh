#!/usr/bin/env bash
# 画像処理 Lambda をビルドして zip にまとめる。
#
# LocalStack に読み込ませるため、docker compose を起動する前に実行する。
# AWS へデプロイするときも同じ成果物を使う。
set -euo pipefail

cd "$(dirname "$0")/.."
OUT="$PWD/docker/artifacts"
mkdir -p "$OUT"

echo "imageworker をビルドしています..."
# provided.al2023 ランタイムは実行ファイル名が bootstrap であることを要求する。
# CGO を無効にして静的リンクし、実行環境に共有ライブラリを持ち込まない。
(cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o "$OUT/bootstrap" ./cmd/imageworker)

echo "zip にまとめています..."
# zip コマンドには頼らない。環境によって存在せず（Windows の Git Bash には
# unzip しか無い）、Lambda が要求する実行権限も付けられないため。
# 詳細は backend/cmd/lambdapack の説明にある。
(cd backend && go run ./cmd/lambdapack "$OUT/bootstrap" "$OUT/imageworker.zip")
rm -f "$OUT/bootstrap"

echo "できました: docker/artifacts/imageworker.zip"
