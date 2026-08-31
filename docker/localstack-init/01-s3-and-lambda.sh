#!/bin/bash
# LocalStack の起動完了後に実行される。
#
# 本番（AWS）の構成をローカルで再現する:
#   S3 バケット → PutObject イベント → 画像処理 Lambda
#
# **同じ経路で動かすことに意味がある。** ローカルだけ別の呼び出し方
# （バックエンドから同期的に呼ぶ等）にすると、イベント駆動特有の問題
# （キーの復号、自分の出力での再発火、二重配送）が本番でしか出なくなる。
set -euo pipefail

BUCKET="${STORAGE_S3_BUCKET:-tabilog-media}"
FUNC="tabilog-imageworker"

echo "[init] S3 バケットを作成: $BUCKET"
awslocal s3api create-bucket --bucket "$BUCKET" \
  --create-bucket-configuration LocationConstraint=ap-northeast-1 2>/dev/null \
  || awslocal s3api create-bucket --bucket "$BUCKET"

# ブラウザから直接アップロードするため CORS を許可する。
echo "[init] CORS を設定"
awslocal s3api put-bucket-cors --bucket "$BUCKET" --cors-configuration '{
  "CORSRules": [{
    "AllowedOrigins": ["http://localhost:5173", "http://localhost:4173"],
    "AllowedMethods": ["PUT", "GET"],
    "AllowedHeaders": ["*"],
    "ExposeHeaders": ["ETag"],
    "MaxAgeSeconds": 3000
  }]
}'

# 確定しなかったアップロードを期限で削除する。
#
# **接頭辞だけでなくタグでも絞る。** originals/ を無条件に消すと、
# 投稿に使われた原本まで消えて別解像度を後から作れなくなる。
# 確定していないものに state=pending が付いており、投稿が確定すると
# アプリが kept に変えて対象から外す。
#
# **infra/s3.tf と同じ条件にしておくこと。** 食い違うと
# ローカルで再現しない不具合が AWS でだけ出る。
echo "[init] ライフサイクルルールを設定（originals/ かつ state=pending の期限削除）"
awslocal s3api put-bucket-lifecycle-configuration --bucket "$BUCKET" --lifecycle-configuration '{
  "Rules": [{
    "ID": "expire-unconfirmed-originals",
    "Status": "Enabled",
    "Filter": { "And": {
      "Prefix": "originals/",
      "Tags": [{ "Key": "state", "Value": "pending" }]
    } },
    "Expiration": { "Days": 1 }
  }]
}'

if [ ! -f /etc/localstack/artifacts/imageworker.zip ]; then
  echo "[init] imageworker.zip が無いため Lambda は作成しない"
  echo "[init]   docker/build-imageworker.sh を実行してから localstack を再起動すること"
  exit 0
fi

echo "[init] 画像処理 Lambda を作成"
awslocal lambda create-function \
  --function-name "$FUNC" \
  --runtime provided.al2023 \
  --handler bootstrap \
  --role arn:aws:iam::000000000000:role/lambda-role \
  --zip-file fileb:///etc/localstack/artifacts/imageworker.zip \
  --timeout 60 \
  --memory-size 512 \
  --environment "Variables={
    DB_HOST=mysql,
    DB_PORT=3306,
    DB_NAME=${DB_NAME},
    DB_USER=${DB_USER},
    DB_PASSWORD=${DB_PASSWORD},
    JWT_SECRET=${JWT_SECRET},
    STORAGE_S3_BUCKET=${BUCKET},
    STORAGE_S3_REGION=ap-northeast-1,
    STORAGE_S3_ENDPOINT=http://localstack:4566,
    AWS_ACCESS_KEY_ID=test,
    AWS_SECRET_ACCESS_KEY=test
  }" >/dev/null

awslocal lambda wait function-active-v2 --function-name "$FUNC"

echo "[init] S3 のイベント通知を設定"
FUNC_ARN=$(awslocal lambda get-function --function-name "$FUNC" --query 'Configuration.FunctionArn' --output text)
awslocal s3api put-bucket-notification-configuration --bucket "$BUCKET" --notification-configuration "{
  \"LambdaFunctionConfigurations\": [{
    \"LambdaFunctionArn\": \"$FUNC_ARN\",
    \"Events\": [\"s3:ObjectCreated:*\"],
    \"Filter\": { \"Key\": { \"FilterRules\": [{ \"Name\": \"prefix\", \"Value\": \"originals/\" }] } }
  }]
}"

echo "[init] 完了"
