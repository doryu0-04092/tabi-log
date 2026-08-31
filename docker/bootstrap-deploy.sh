#!/usr/bin/env bash
# apply したあとの初回構築。**apply だけではアプリは動かない。**
#
# terraform が作るのは器だけである。ECR は空、S3 も空、スキーマも無い。
# この状態の ECS サービスはイメージを取得できず、タスクが起動しては
# 落ちるのを繰り返す。**異常ではなく、まだ中身が無いだけである。**
#
# やること:
#   1. 画像処理の zip を build する（**terraform はビルドしない**）
#   2. バックエンドのイメージを build して ECR へ push（タグ initial）
#   3. マイグレーションを流す（run-migrate.sh に委ねる）
#   4. ECS を再デプロイして、新しいイメージで起動させる
#   5. フロントエンドを build して S3 へ置く
#   6. 公開されている経路で応答を確かめる
#
# **順序を変えないこと。**
#   - マイグレーションより先にアプリを起動させると、
#     スキーマが無い状態で readyz が通らず、置換が延々と走る
#   - フロントを先に置くと、まだ動かない API を叩く画面が公開される
#
# 使い方:
#   bash docker/bootstrap-deploy.sh
#
# **何度実行してもよい。** 途中で失敗したら直して同じものを流す。
set -euo pipefail

cd "$(dirname "$0")/.."

# 使う道具が揃っているかを先に見る。
#
# **途中で落ちても害は無いが、原因が分かりにくい。** 例えば Docker が
# 動いていないと docker build のエラー文言で落ちる。それだけを見て
# 「何が起きたか」に辿り着くには、この手順を知っている必要がある。
#
# **何も進んでいない状態で止めるのが最も安い。**
missing=()
for c in aws docker jq npm terraform curl; do
  command -v "$c" >/dev/null || missing+=("$c")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "次のコマンドが見つかりません: ${missing[*]}" >&2
  exit 1
fi

# **Docker は「入っている」だけでは足りない。** Docker Desktop が
# 起動していないと、コマンドはあるのに接続できない。
if ! docker info >/dev/null 2>&1; then
  echo "Docker に接続できません。Docker Desktop を起動してください。" >&2
  echo "（コマンドはありますが、デーモンが動いていません）" >&2
  exit 1
fi

# terraform の出力から必要な値を取る。
#
# **apply が終わっていることが前提である。** 終わっていなければ
# ここで落ちる（出力が無い）。中途半端に進めるより良い。
pushd infra >/dev/null
ECR=$(terraform output -raw ecr_repository_url)
CLUSTER=$(terraform output -raw ecs_cluster)
SERVICE=$(terraform output -raw ecs_service)
STATIC_BUCKET=$(terraform output -raw static_bucket)
# **app_url はスキームを含む。** cloudfront_domain という出力は無い。
# 以前 https://https:// になった不具合と同じ形なので、
# ここで足さないこと（docs/progress.md の #105）。
APP_URL=$(terraform output -raw app_url)
REGION=$(terraform output -raw region 2>/dev/null || echo "ap-northeast-1")
popd >/dev/null

echo "==> 対象"
echo "    ECR:      $ECR"
echo "    クラスタ: $CLUSTER / $SERVICE"
echo "    静的:     $STATIC_BUCKET"
echo "    配信:     $APP_URL"
echo

# ---------------------------------------------------------------------------
# 1. 画像処理の zip
# ---------------------------------------------------------------------------
#
# **terraform apply は zip を配るだけで、Go のビルドをしない。**
# 古い zip が残っていると、それがそのまま Lambda に載る。
#
# 2026-08-31 に実際に踏んだ。JWT_SECRET を環境変数から外したのに、
# それを不要にするコードが Lambda に届いておらず、画像処理が
# 起動時に落ち続けた。**apply は成功し、plan にも差分は出ない。**
# 気づけるのは画像を1枚上げてみたときだけである。
echo "==> 画像処理の zip を build します"
bash docker/build-imageworker.sh
echo

# **zip が変わったら apply が要る。** ここでは判断できないため、
# 変わっていれば知らせるに留める（apply はこのスクリプトの仕事ではない）。
if ! git diff --quiet --stat -- docker/artifacts/imageworker.zip 2>/dev/null; then
  echo "    zip が変わりました。**terraform apply が要ります。**"
  echo "    このまま進めますが、画像処理は古いままです。"
  echo
fi

# ---------------------------------------------------------------------------
# 2. バックエンドのイメージ
# ---------------------------------------------------------------------------
#
# タグは initial。**infra/variables.tf の image_tag と揃っている必要がある。**
# 揃っていないと、ECS は存在しないタグを取りに行き続ける。
#
# CD が積むのは sha-<コミットSHA> であり、こちらは初回だけのものである。
# ECR のライフサイクルは sha- で始まるものしか消さないため、
# initial は残り続ける（docs/audit-2026-08-31.md L5）。
echo "==> バックエンドのイメージを build して push します"
aws ecr get-login-password --region "$REGION" \
  | docker login --username AWS --password-stdin "${ECR%%/*}" >/dev/null

# **--provenance=false --sbom=false を外さないこと。**
# 付けないと buildx が attestation を含むイメージインデックスを作り、
# Fargate が取得できないことがある。
docker build --platform linux/amd64 --provenance=false --sbom=false \
  -f docker/Dockerfile.backend -t "$ECR:initial" .
docker push "$ECR:initial"
echo "    ${ECR}:initial"
echo

# ---------------------------------------------------------------------------
# 3. マイグレーション
# ---------------------------------------------------------------------------
#
# **データベースはプライベートサブネットにあり、手元からは届かない。**
# run-migrate.sh が ECS のタスクとして VPC 内で1回だけ動かす。
echo "==> マイグレーションを流します"
bash docker/run-migrate.sh up
echo

# ---------------------------------------------------------------------------
# 4. ECS の再デプロイ
# ---------------------------------------------------------------------------
#
# イメージを push しただけでは切り替わらない。ECS は失敗したタスクの
# 再起動を、間隔を空けながら繰り返している。**待つと数分かかる。**
# 明示的に新しいデプロイを始めさせる。
echo "==> ECS を再デプロイします"
aws ecs update-service --cluster "$CLUSTER" --service "$SERVICE" \
  --force-new-deployment >/dev/null

echo "==> 安定するまで待ちます（数分かかります）"
# **ここで失敗したら先へ進まない。** 起動しないアプリに向けて
# フロントを公開しても、確かめられることが増えない。
#
# 起動しない理由で多いのは checkProduction である（audit L1）。
# 本番の4条件が1つでも欠けると、意図してアプリを落とす。
if ! aws ecs wait services-stable --cluster "$CLUSTER" --services "$SERVICE"; then
  echo
  echo "安定しませんでした。ログを確認してください:" >&2
  echo "  aws logs tail /ecs/${CLUSTER} --since 10m" >&2
  exit 1
fi
echo "    安定しました"
echo

# ---------------------------------------------------------------------------
# 5. フロントエンド
# ---------------------------------------------------------------------------
#
# 扱いを3つに分ける。**名前が変わるものと変わらないものが混ざっている。**
# 詳細は .github/workflows/deploy.yml と同じ考え方
# （docs/audit-2026-08-31.md L6）。
echo "==> フロントエンドを build して置きます"
pushd frontend >/dev/null
npm ci
npm run build

# ハッシュ付きでないものを先に。**--delete はここでしか効かない**
# （除外したものは削除の対象にもならない）。
aws s3 sync build/ "s3://$STATIC_BUCKET/" --delete \
  --exclude index.html \
  --exclude "_app/immutable/*" \
  --cache-control "public,max-age=300"
aws s3 sync build/_app/immutable/ "s3://$STATIC_BUCKET/_app/immutable/" --delete \
  --cache-control "public,max-age=31536000,immutable"
aws s3 cp build/index.html "s3://$STATIC_BUCKET/index.html" \
  --cache-control "no-cache"
popd >/dev/null
echo

# ---------------------------------------------------------------------------
# 6. 公開されている経路で確かめる
# ---------------------------------------------------------------------------
#
# **ECS が安定しただけでは足りない。** CloudFront → ALB → アプリ の
# 経路が通っている保証にならない。実際に外から叩く。
echo "==> 公開されている経路を確かめます"
for path in /api/livez /api/readyz /; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "${APP_URL}${path}" || echo "000")
  echo "    ${path} → ${code}"
  if [ "$code" != "200" ]; then
    echo
    echo "経路が通っていません（${path} が ${code}）。" >&2
    echo "readyz が落ちる場合、多いのはマイグレーション未了と接続設定です。" >&2
    exit 1
  fi
done

echo
echo "初回構築が終わりました: ${APP_URL}"
echo
echo "次は docs/deploy-verification.md の 1 章から確認してください。"
echo "**1-B（CSP で黙って壊れうる箇所）はブラウザのコンソールを開いた状態で見ること。**"
