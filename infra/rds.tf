########################################
# RDS（MySQL 8.4）
########################################

resource "aws_db_subnet_group" "main" {
  name       = "${var.project}-db"
  subnet_ids = aws_subnet.private[*].id

  tags = { Name = "${var.project}-db" }
}

# **パラメータグループを自分で作る理由は ngram_token_size である。**
#
# 日本語は空白で区切られないため、全文検索の既定パーサでは機能しない。
# ngram パーサをトークン長2で使う前提で設計しており、
# **ローカルの docker compose と同じ値でなければ検索結果が変わる。**
#
# この値は static なので、変えるには再起動に加えて
# **既存の全文索引を作り直す必要がある。**
# 稼働後に変えるのは高くつくため、2 を動かさない前提で設計している。
resource "aws_db_parameter_group" "main" {
  name   = "${var.project}-mysql84"
  family = "mysql8.4"

  parameter {
    name         = "ngram_token_size"
    value        = "2"
    apply_method = "pending-reboot"
  }

  # 文字集合をローカルと揃える。揃えないと、
  # 同じ SQL が同じ結果を返さない（照合順序が変われば比較も変わる）。
  parameter {
    name  = "character_set_server"
    value = "utf8mb4"
  }

  parameter {
    name  = "collation_server"
    value = "utf8mb4_0900_ai_ci"
  }

  # アプリケーションは日時を UTC で扱う。
  # サーバーのタイムゾーン設定に結果が左右されないよう固定する。
  parameter {
    name  = "time_zone"
    value = "UTC"
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_db_instance" "main" {
  identifier = "${var.project}-db"

  engine         = "mysql"
  engine_version = var.db_engine_version
  instance_class = var.db_instance_class

  allocated_storage = var.db_allocated_storage
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = var.db_name
  username = var.db_username
  password = random_password.db.result

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.db.id]
  parameter_group_name   = aws_db_parameter_group.main.name

  # **公開しない。** プライベートサブネットに置いたうえで、
  # ここでも明示的に無効にする。
  publicly_accessible = false

  # ---------------------------------------------------------------------
  # 以下は「学習用の割り切り」である。本番運用では変える。
  # ---------------------------------------------------------------------

  # 冗長化しない。片方のゾーンが落ちれば止まる。
  multi_az = false

  # 保持は1日だけ。**0 にはしない**（0 にすると自動バックアップが
  # 完全に無効になり、ある時点への復元ができなくなる）。
  backup_retention_period = 1

  # **消せる状態にしてある。** 使わない期間は destroy する運用を前提と
  # しているため、削除保護と最終スナップショットを外している。
  # 本番ではどちらも有効にする。
  deletion_protection       = false
  skip_final_snapshot       = true
  final_snapshot_identifier = null

  # マイナーバージョンの自動更新は入れる。
  # 更新の窓を絞れるほどの運用体制が無い以上、
  # 古いまま放置するより自動で当たるほうが安全である。
  #
  # **db_engine_version をマイナー版まで固定しないこと。**
  # 固定すると、自動更新が走った直後の plan が版を戻す差分を出す。
  auto_minor_version_upgrade = true

  # パラメータグループの変更（static）を即時に効かせる。
  apply_immediately = true

  tags = { Name = "${var.project}-db" }
}

# ---------------------------------------------------------------------------
# 接続数の検算
# ---------------------------------------------------------------------------

# **「タスク数 × プール上限 ≦ max_connections」を満たす必要がある。**
#
# db.t4g.micro（1GB）の max_connections は既定の式
# {DBInstanceClassMemory/12582880} でおよそ 85 になる。
# 1タスク × 25 = 25 は収まるが、**タスクやクラスを変えるときは
# ここが上限の根拠**になる（見積もりは db_estimated_max_connections）。
#
# 計算で止めるのではなく、超える構成を書いたときに気づけるようにしておく。
# **Lambda も同じ RDS に繋ぐ。** ECS だけを数えると、
# 画像処理が同時に走ったぶんが計算から漏れる。
locals {
  ecs_connections    = var.desired_count * var.db_max_connections_headroom
  lambda_connections = var.imageworker_concurrency * var.imageworker_db_connections

  planned_connections = local.ecs_connections + local.lambda_connections
}

resource "terraform_data" "connection_budget" {
  # **plan の時点で落ちる。** apply してから気づくのでは遅い。
  lifecycle {
    precondition {
      condition = local.planned_connections <= var.db_estimated_max_connections
      error_message = format(
        "接続数が上限を超える見込みです: タスク %d × プール %d = %d、Lambda %d × プール %d = %d、合計 %d > 約%d。タスク数か Lambda の同時実行数を減らすか、インスタンスクラスを上げて db_estimated_max_connections も上げてください。",
        var.desired_count,
        var.db_max_connections_headroom,
        local.ecs_connections,
        var.imageworker_concurrency,
        var.imageworker_db_connections,
        local.lambda_connections,
        local.planned_connections,
        var.db_estimated_max_connections,
      )
    }
  }
}
