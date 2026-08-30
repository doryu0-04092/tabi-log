#!/bin/sh
# DSN をコンテナの中で組み立てて migrate を実行する。
#
# **パスワードを引数やタスク定義に置かないための入口である。**
# DB_PASSWORD は ECS の secrets 経由で SSM から注入される。
set -eu

: "${DB_HOST:?DB_HOST is required}"
: "${DB_PORT:=3306}"
: "${DB_NAME:?DB_NAME is required}"
: "${DB_USER:?DB_USER is required}"
: "${DB_PASSWORD:?DB_PASSWORD is required}"

# **パスワードは URL エンコードする。**
#
# 生成されるパスワードには % # ? & = : などが含まれる
# (infra/secrets.tf の override_special = "!#$%&*()-_=+[]{}<>:?")。
# golang-migrate の -database は mysql:// を **URL として解析する**ため、
# 素のまま埋めると壊れる。実際に踏んだ(2026-08-30):
#
#     error: failed to open database: invalid URL escape "%YQ"
#
# **アプリ本体は壊れない**点に注意。Go の mysql ドライバが使う DSN は
# URL ではないため、同じパスワードでもそのまま通る。
# ここだけが URL 形式を要求している。
urlencode() {
	printf '%s' "$1" | awk '
		BEGIN { for (i = 0; i < 256; i++) ord[sprintf("%c", i)] = i }
		{
			n = length($0)
			for (i = 1; i <= n; i++) {
				c = substr($0, i, 1)
				if (c ~ /[A-Za-z0-9._~-]/) printf "%s", c
				else printf "%%%02X", ord[c]
			}
		}'
}

ENCODED_USER=$(urlencode "$DB_USER")
ENCODED_PASSWORD=$(urlencode "$DB_PASSWORD")

# multiStatements=true が要る。1ファイルに複数文を書いた移行があるため。
DSN="mysql://${ENCODED_USER}:${ENCODED_PASSWORD}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}?multiStatements=true"

# **パスワードを出力しない。** 接続先だけを残す。
echo "migrate: ${DB_USER}@${DB_HOST}:${DB_PORT}/${DB_NAME} に対して [$*] を実行します"

exec migrate -path=/migrations -database "$DSN" "$@"
