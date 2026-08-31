########################################
# ECR
########################################

resource "aws_ecr_repository" "backend" {
  name = var.project

  # push のたびに脆弱性を見る。追加費用はかからない。
  image_scanning_configuration {
    scan_on_push = true
  }

  # **同じタグへの push を許す。** 学習中はやり直しが多く、
  # 禁止すると毎回タグを変えることになる。
  # 本番では IMMUTABLE にして、動いている版が変わらないようにする。
  image_tag_mutability = "MUTABLE"

  force_delete = true

  tags = { Name = var.project }
}

# 古いイメージを消す。**放っておくと push した数だけ課金が増える。**
resource "aws_ecr_lifecycle_policy" "backend" {
  repository = aws_ecr_repository.backend.name

  # **CD が積むイメージだけを削る。**
  #
  # 以前は tagStatus = "any" で「直近10件」だったが、それでは
  # migrate と initial のイメージも巻き込む。どちらも Terraform が
  # 名指しで参照しており（migrate.tf / ecs.tf）、消えると
  # マイグレーションもタスクの起動もできなくなる。
  #
  # CD が積むのはコミットハッシュのタグである。それだけを対象にする。
  # **タグの付け方を変えるときは、ここも変えること。**
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "タグの無いイメージは1日で消す（積み損ねた残骸）"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "CD が積んだイメージは直近10件だけ残す"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = [var.cd_image_tag_prefix]
          countType     = "imageCountMoreThan"
          countNumber   = 10
        }
        action = { type = "expire" }
      },
    ]
  })
}

########################################
# ECS
########################################

resource "aws_ecs_cluster" "main" {
  name = var.project

  setting {
    name  = "containerInsights"
    value = "disabled"
  }

  tags = { Name = var.project }
}

resource "aws_cloudwatch_log_group" "backend" {
  name = "/ecs/${var.project}"

  # **無期限にしない。** 消さないログは増え続け、取り込み量ではなく
  # 保存量で課金され続ける。
  retention_in_days = 14

  tags = { Name = var.project }
}

resource "aws_ecs_task_definition" "backend" {
  family                   = var.project
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory

  execution_role_arn = aws_iam_role.execution.arn
  task_role_arn      = aws_iam_role.task.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }

  container_definitions = jsonencode([{
    name      = "backend"
    image     = "${aws_ecr_repository.backend.repository_url}:${var.image_tag}"
    essential = true

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    # **秘密は environment に置かない。** ここに書くとタスク定義に
    # 平文で残り、コンソールからも API からも読める。
    environment = [
      { name = "APP_ENV", value = "production" },
      { name = "PORT", value = "8080" },
      { name = "LOG_LEVEL", value = "info" },

      { name = "DB_HOST", value = aws_db_instance.main.address },
      { name = "DB_PORT", value = "3306" },
      { name = "DB_NAME", value = var.db_name },
      { name = "DB_USER", value = var.db_username },
      { name = "DB_MAX_OPEN_CONNS", value = tostring(var.db_max_connections_headroom) },

      { name = "STORAGE_S3_BUCKET", value = aws_s3_bucket.images.id },
      { name = "STORAGE_S3_REGION", value = var.region },

      # **画像は CloudFront から配る。**
      # この3つが揃うと、表示用 URL が署名付き URL から
      # https://<配信ドメイン>/variants/<鍵> に変わり、
      # 読む権利は署名付き Cookie が持つようになる。
      # 揃っていないとアプリが起動時に落ちる（設定漏れを黙って見逃さない）。
      { name = "CDN_DOMAIN", value = aws_cloudfront_distribution.main.domain_name },
      { name = "CDN_KEY_PAIR_ID", value = aws_cloudfront_public_key.cdn.id },

      # **HTTPS でのみ Cookie を送らせる。** CloudFront が終端しており、
      # 利用者との間は常に HTTPS である。
      { name = "COOKIE_SECURE", value = "true" },

      # **ALB の背後にいるため、発信元は X-Forwarded-For で判断する。**
      # これを false のままにすると、レート制限の鍵が
      # 全リクエストで ALB の IP になり、まったく効かなくなる。
      { name = "TRUST_PROXY_HEADERS", value = "true" },
      # CloudFront → ALB → アプリ の2段。**構成の段数を変えたら必ず変える。**
      { name = "TRUSTED_PROXY_HOPS", value = "1" },
    ]

    secrets = [
      { name = "DB_PASSWORD", valueFrom = aws_ssm_parameter.db_password.arn },
      { name = "JWT_SECRET", valueFrom = aws_ssm_parameter.jwt_secret.arn },
      { name = "CDN_PRIVATE_KEY", valueFrom = aws_ssm_parameter.cdn_private_key.arn },
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.backend.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "backend"
      }
    }

    # コンテナ自身にも生存確認を持たせる。
    # **ここでもデータベースを見ない**（理由は alb.tf と同じ）。
    healthCheck = {
      command     = ["CMD-SHELL", "wget -q -O - http://localhost:8080/api/livez || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 20
    }
  }])

  tags = { Name = var.project }
}

resource "aws_ecs_service" "backend" {
  name            = var.project
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.backend.arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = aws_subnet.public[*].id
    security_groups = [aws_security_group.tasks.id]
    # ECR と SSM へ出るために要る。受信はセキュリティグループで閉じている。
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = "backend"
    container_port   = 8080
  }

  # **壊れたものを出したまま完了させない。**
  # 新しいタスクが healthy にならなければ、自動で前の版へ戻す。
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  # 起動直後は healthy になるまで猶予を与える。
  # 短すぎると、起動が終わる前に不健全と判定して置き換え続ける。
  health_check_grace_period_seconds = 60

  # リスナールールより先にサービスを作ると、
  # ターゲットグループが ALB に紐づいておらず作成に失敗する。
  depends_on = [aws_lb_listener_rule.from_cloudfront]

  tags = { Name = var.project }
  # **CD が登録したタスク定義を巻き戻さない。**
  #
  # CD（.github/workflows/deploy.yml）は、現行のタスク定義のイメージだけを
  # 差し替えた新しいリビジョンを登録し、サービスをそこへ向ける。
  # 一方このリソースは task_definition に Terraform が作ったリビジョンを指定している。
  #
  # ignore_changes を付けないと、デプロイ後の terraform apply が
  # **サービスを Terraform 側のリビジョンへ黙って戻す。**
  # 障害時にロールバックしていた場合は、それを勝手に取り消して
  # 再び壊れた版を配ることになる。
  #
  # 所有者を分ける: サービスの構成（ネットワーク・LB・サーキットブレーカー）は
  # Terraform、**どのリビジョンが動いているかは CD**。
  lifecycle {
    ignore_changes = [task_definition]
  }
}
