########################################
# マイグレーションを流すためのタスク
########################################

# **データベースはプライベートサブネットにあり、手元からは届かない。**
# そのため migrate を VPC 内で1回だけ動かす。
# infra/README.md が挙げている2案のうち「ECS のタスクから流す」を実装したもの。
#
# **サービスではなくタスク定義だけを置く。** 常駐させるものではなく、
# `aws ecs run-task` で必要なときに1回動かして終わる。
#
# **起動時の自動適用はしない**という設計判断は変えていない
# (スキーマの変更をデプロイから切り離すため)。これはその「切り離した側」の手段である。

resource "aws_cloudwatch_log_group" "migrate" {
  name              = "/ecs/${var.project}-migrate"
  retention_in_days = 14

  tags = { Name = "${var.project}-migrate" }
}

resource "aws_ecs_task_definition" "migrate" {
  family                   = "${var.project}-migrate"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"

  # 移行は一瞬で終わる。バックエンド本体より小さくてよい。
  cpu    = "256"
  memory = "512"

  # ECR からの取得と SSM からの秘密取得に要る。バックエンドと同じもので足りる。
  execution_role_arn = aws_iam_role.execution.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }

  container_definitions = jsonencode([{
    name      = "migrate"
    image     = "${aws_ecr_repository.backend.repository_url}:${var.migrate_image_tag}"
    essential = true

    # **DSN を渡さない。** 引数やここに書くとタスク定義にパスワードが平文で残り、
    # コンソールからも API からも読めてしまう。
    # コンテナの中で組み立てる(docker/migrate-entrypoint.sh)。
    environment = [
      { name = "DB_HOST", value = aws_db_instance.main.address },
      { name = "DB_PORT", value = "3306" },
      { name = "DB_NAME", value = var.db_name },
      { name = "DB_USER", value = var.db_username },
    ]

    secrets = [
      { name = "DB_PASSWORD", valueFrom = aws_ssm_parameter.db_password.arn },
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.migrate.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "migrate"
      }
    }
  }])

  tags = { Name = "${var.project}-migrate" }
}
