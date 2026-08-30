#!/usr/bin/env bash
# マイグレーションを AWS 上で流す。
#
# **データベースはプライベートサブネットにあり、手元からは届かない。**
# migrate を ECS タスクとして VPC 内で1回だけ動かす
# (infra/README.md が挙げている2案のうち「ECS のタスクから流す」)。
#
# やること:
#   1. マイグレーション用イメージを build して ECR へ push
#   2. run-task で1回起動
#   3. 終了まで待ち、**終了コードとログを必ず表示する**
#
# 使い方:
#   bash docker/run-migrate.sh up        # 適用（既定）
#   bash docker/run-migrate.sh version   # 現在の版
#   bash docker/run-migrate.sh down 1    # 1つ戻す
set -euo pipefail

# **Git Bash が先頭スラッシュを Windows のパスへ変換するのを止める。**
# これが無いと、ロググループ名 /ecs/tabilog-migrate が
# C:/Program Files/Git/ecs/... に化けて、失敗したときのログが取れない。
# 実際にそれで1往復無駄にした(2026-08-30)。
export MSYS_NO_PATHCONV=1

cd "$(dirname "$0")/.."
if [ "$#" -eq 0 ]; then set -- up; fi
ARGS=("$@")

echo "==> Terraform の出力から接続先を取得します"
pushd infra >/dev/null
ECR=$(terraform output -raw ecr_repository_url)
CLUSTER=$(terraform output -raw ecs_cluster)
FAMILY=$(terraform output -raw migrate_task_family)
SUBNETS=$(terraform output -json private_subnets_for_tasks | tr -d '[]" \n')
SG=$(terraform output -raw tasks_security_group)
REGION=$(terraform output -raw region 2>/dev/null || echo "ap-northeast-1")
popd >/dev/null

echo "==> イメージを build して push します"
aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "${ECR%%/*}" >/dev/null

# **--provenance=false --sbom=false を外さないこと。**
# 付けないと buildx が attestation を含むイメージインデックスを作り、
# Fargate が取得できないことがある。
docker build --platform linux/amd64 --provenance=false --sbom=false \
  -f docker/Dockerfile.migrate -t "$ECR:migrate" . >/dev/null
docker push "$ECR:migrate" >/dev/null
echo "    ${ECR}:migrate"

echo "==> タスクを起動します: ${ARGS[*]}"
CMD_JSON=$(printf '%s\n' "${ARGS[@]}" | jq -R . | jq -sc .)
OVERRIDES=$(printf '{"containerOverrides":[{"name":"migrate","command":%s}]}' "$CMD_JSON")

TASK_ARN=$(aws ecs run-task \
  --cluster "$CLUSTER" \
  --task-definition "$FAMILY" \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[$SUBNETS],securityGroups=[$SG],assignPublicIp=ENABLED}" \
  --overrides "$OVERRIDES" \
  --query 'tasks[0].taskArn' --output text)

echo "    $TASK_ARN"
echo "==> 終了を待ちます"
aws ecs wait tasks-stopped --cluster "$CLUSTER" --tasks "$TASK_ARN"

# **終了コードを必ず見る。** タスクが停止しただけでは成功とは限らない。
# 見落とすと、スキーマが古いままアプリだけ動く状態になる。
read -r EXIT_CODE STOPPED_REASON < <(aws ecs describe-tasks \
  --cluster "$CLUSTER" --tasks "$TASK_ARN" \
  --query 'tasks[0].[containers[0].exitCode,stoppedReason]' --output text)

echo "==> ログ"
TASK_ID="${TASK_ARN##*/}"
LOG_GROUP="/ecs/${FAMILY}"
# **ストリーム名を決め打ちしない。** 命名が変わると黙って空になり、
# 「ログが無い」のか「取り方を間違えた」のか区別できなくなる。
STREAM=$(aws logs describe-log-streams \
  --log-group-name "$LOG_GROUP" \
  --log-stream-name-prefix "migrate/migrate/${TASK_ID}" \
  --query 'logStreams[0].logStreamName' --output text 2>/dev/null || true)

if [ -n "${STREAM:-}" ] && [ "$STREAM" != "None" ]; then
  aws logs get-log-events --log-group-name "$LOG_GROUP" --log-stream-name "$STREAM" \
    --query 'events[].message' --output text 2>/dev/null | tr '\t' '\n' | sed 's/^/    /'
else
  echo "    (ログストリームが見つかりません: ${LOG_GROUP} / migrate/migrate/${TASK_ID})"
fi

echo
if [ "$EXIT_CODE" = "0" ]; then
  echo "成功しました（終了コード 0）"
else
  echo "失敗しました（終了コード ${EXIT_CODE}／理由: ${STOPPED_REASON}）"
  exit 1
fi
