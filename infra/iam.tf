########################################
# IAM
########################################

# ---------------------------------------------------------------------------
# ECS タスクの実行ロール（タスクを起動するのは ECS 自身）
# ---------------------------------------------------------------------------

# **実行ロールとタスクロールは別物である。**
# 実行ロールは「ECS がイメージを取り、秘密を読み、ログを出す」ために使う。
# タスクロールは「動いているアプリケーションが AWS を呼ぶ」ために使う。
# 混ぜると、アプリケーションに ECR の権限まで渡ることになる。

data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "execution" {
  name               = "${var.project}-ecs-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "execution_managed" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# 秘密の読み取りは管理ポリシーに含まれない。**自分のものだけに絞る。**
data "aws_iam_policy_document" "execution_secrets" {
  statement {
    effect  = "Allow"
    actions = ["ssm:GetParameters"]
    resources = [
      aws_ssm_parameter.db_password.arn,
      aws_ssm_parameter.jwt_secret.arn,
      aws_ssm_parameter.cdn_private_key.arn,
    ]
  }
}

resource "aws_iam_role_policy" "execution_secrets" {
  name   = "read-secrets"
  role   = aws_iam_role.execution.id
  policy = data.aws_iam_policy_document.execution_secrets.json
}

# ---------------------------------------------------------------------------
# ECS タスクロール（動いているアプリケーション）
# ---------------------------------------------------------------------------

resource "aws_iam_role" "task" {
  name               = "${var.project}-ecs-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

# アプリケーションが S3 に対して行うのは3つだけである。
#
# - 署名付き PUT の発行（PutObject）
# - 表示用の署名付き URL の発行（GetObject）
# - 削除（投稿の削除・退会）
#
# **バケットの一覧や設定の変更は要らない。** 与えない。
data "aws_iam_policy_document" "task_s3" {
  statement {
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = ["${aws_s3_bucket.images.arn}/*"]
  }
}

resource "aws_iam_role_policy" "task_s3" {
  name   = "images"
  role   = aws_iam_role.task.id
  policy = data.aws_iam_policy_document.task_s3.json
}

# ---------------------------------------------------------------------------
# 画像処理 Lambda のロール
# ---------------------------------------------------------------------------

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project}-imageworker"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

# **VPC の中で動かすための権限。** これが無いと関数の作成そのものが失敗する。
# ログ出力の権限もこの管理ポリシーに含まれる。
resource "aws_iam_role_policy_attachment" "lambda_vpc" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

# 原本を読み、変換したものを書く。
#
# **削除は与えない。** この関数が消すべきものは無く、
# 与えれば不具合が起きたときに消えてしまう。
data "aws_iam_policy_document" "lambda_s3" {
  statement {
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = ["${aws_s3_bucket.images.arn}/*"]
  }
}

resource "aws_iam_role_policy" "lambda_s3" {
  name   = "images"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_s3.json
}
