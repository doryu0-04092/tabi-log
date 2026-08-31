########################################
# 画像処理 Lambda
########################################

# **アップロードはブラウザから S3 へ直接送るため、バックエンドは
# ファイルの中身を一度も見ない。** 検証と加工はここが担う。
#
#   1. マジックバイトの検証（拡張子も申告された Content-Type も信用しない）
#   2. 寸法とサイズの上限確認
#   3. 再エンコードによる EXIF の全除去 ← GPS 座標がここで消える
#   4. 変換物の生成（thumb 320 / medium 1080）
#   5. media.status を processed にする
#
# **処理が成功した画像だけが投稿として公開できる。**
# 「Lambda が落ちたが投稿は公開され、GPS 入りの原本が配信される」経路を
# 構造的に作らないための順序である。

# ビルド成果物。**リポジトリには置かない。**
# 実行のたびに変わるものであり、差分として意味が無い。
#
#   bash docker/build-imageworker.sh
#
# を先に実行しておく。無ければ apply の時点で「ファイルが無い」と落ちる。
locals {
  imageworker_zip = "${path.module}/../docker/artifacts/imageworker.zip"
}

resource "aws_cloudwatch_log_group" "imageworker" {
  name              = "/aws/lambda/${var.project}-imageworker"
  retention_in_days = 14

  tags = { Name = "${var.project}-imageworker" }
}

resource "aws_lambda_function" "imageworker" {
  function_name = "${var.project}-imageworker"
  role          = aws_iam_role.lambda.arn

  # provided.al2023 は実行ファイル名が bootstrap であることを要求する。
  # ビルドスクリプトがその名前で固めている。
  runtime = "provided.al2023"
  handler = "bootstrap"

  filename = local.imageworker_zip
  # **中身が変わったときだけ入れ替える。** ファイル名だけを見ると、
  # 作り直しても Terraform は変化に気づかない。
  source_code_hash = filebase64sha256(local.imageworker_zip)

  architectures = ["x86_64"]

  # 画像の再エンコードは CPU を使う。**メモリを増やすと CPU も増える**
  # （Lambda は割り当てメモリに比例して CPU が配分される）ため、
  # 512MB では 1080px への変換で時間がかかる。
  memory_size = 1024
  timeout     = 60

  # **同時実行数に上限を置く。** 置かないとアカウントの上限まで増え、
  # そのぶん RDS への接続も増える。署名付き URL の発行には上限を
  # 設けたが、それは1利用者あたりであり、全体の歯止めにはならない。
  #
  # 予約した数はアカウントの共有枠から差し引かれる。
  # 他に Lambda が無いため影響しない。
  reserved_concurrent_executions = var.imageworker_concurrency

  # **VPC の中に置く。** データベースを更新する必要があるためである。
  # その代償として、S3 へはゲートウェイエンドポイント経由で出る
  # （network.tf。これが無いと S3 に到達できず毎回タイムアウトする）。
  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      APP_ENV   = "production"
      LOG_LEVEL = "info"

      DB_HOST = aws_db_instance.main.address
      DB_PORT = "3306"
      DB_NAME = var.db_name
      DB_USER = var.db_username
      # **ここだけ Parameter Store を使わない。**
      #
      # プライベートサブネットには NAT が無く、SSM へ到達するには
      # インターフェース型のエンドポイント（時間課金）が要る。
      # 画像処理のためだけに常時課金を足す判断は取らない。
      #
      # 代償: 値が Lambda の設定に入り、権限のある人はコンソールから読める。
      # Terraform の state に平文で入るのと同じ性質の割り切りである。
      DB_PASSWORD = random_password.db.result

      # **JWT_SECRET は渡さない。** 画像処理はトークンを発行も検証も
      # しない。config.LoadFor(RoleImageWorker) が必須から外している。
      # 使わない秘密を配れば、読める場所がその分だけ増える。

      # **接続プールを絞る。** 既定の 25 は API サーバー向けの値で、
      # 画像処理は1回の呼び出しで数回しか問い合わせない。同時実行の
      # ぶんだけ倍になるため、絞らないと RDS の接続上限を
      # Lambda 側だけで使い切りうる。
      DB_MAX_OPEN_CONNS = tostring(var.imageworker_db_connections)

      STORAGE_S3_BUCKET = aws_s3_bucket.images.id
      STORAGE_S3_REGION = var.region
    }
  }

  logging_config {
    log_format = "Text"
    log_group  = aws_cloudwatch_log_group.imageworker.name
  }

  depends_on = [
    aws_iam_role_policy_attachment.lambda_vpc,
    aws_cloudwatch_log_group.imageworker,
  ]

  tags = { Name = "${var.project}-imageworker" }
}

# S3 がこの関数を呼べるようにする。
#
# **source_arn を絞る。** 絞らないと、他のバケットからも呼べてしまう。
resource "aws_lambda_permission" "from_s3" {
  statement_id  = "AllowExecutionFromS3"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.imageworker.function_name
  principal     = "s3.amazonaws.com"
  source_arn    = aws_s3_bucket.images.arn
  # 同じアカウントのバケットに限る。
  source_account = data.aws_caller_identity.current.account_id
}

########################################
# 画像処理が失敗したときに気づく
########################################

# S3 → Lambda は非同期である。**呼び出し側は結果を知らない。**
# 既定では2回再試行したあと、失敗はどこにも残らずに捨てられる。
#
# 画面は22秒で諦め、media は pending のまま残る。
# **利用者には「処理中のまま終わらない画像」に見え、こちらには
# 何も届かない。** 拾い直す仕組みの前に、まず気づけるようにする。
resource "aws_sqs_queue" "imageworker_dlq" {
  name = "${var.project}-imageworker-dlq"

  # 14 日は SQS の上限。**捨てる理由が無い。** 溜まるのは
  # 失敗した呼び出しだけで、量も費用も問題にならない。
  message_retention_seconds = 1209600

  tags = { Name = "${var.project}-imageworker-dlq" }
}

# **再試行の回数を明示する。** 既定と同じ値だが、書いておかないと
# 「何回試して諦めたのか」がコードから読めない。
resource "aws_lambda_function_event_invoke_config" "imageworker" {
  function_name          = aws_lambda_function.imageworker.function_name
  maximum_retry_attempts = 2

  destination_config {
    on_failure {
      destination = aws_sqs_queue.imageworker_dlq.arn
    }
  }
}

# 溜まったら気づく。
#
# **1件で鳴らす。** 画像処理の失敗は例外的な出来事であり、
# 「何件からが異常か」を決める根拠が無い。件数で閾値を作ると、
# その根拠のない数字が独り歩きする。
resource "aws_cloudwatch_metric_alarm" "imageworker_dlq" {
  alarm_name          = "${var.project}-imageworker-dlq"
  alarm_description   = "画像処理が再試行後も失敗し、DLQ に積まれた。media が pending のまま残っている。"
  namespace           = "AWS/SQS"
  metric_name         = "ApproximateNumberOfMessagesVisible"
  statistic           = "Maximum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"

  # **データが無い間は鳴らさない。** 失敗が無ければ SQS は
  # メトリクスを出さず、欠測を異常とみなすと常時鳴り続ける。
  treat_missing_data = "notBreaching"

  dimensions = { QueueName = aws_sqs_queue.imageworker_dlq.name }

  alarm_actions = local.alarm_actions
  ok_actions    = local.alarm_actions
}

# Lambda 自体のエラー。**DLQ に積まれる前の段階で見える。**
# 再試行で成功したものはここにしか出ない。
resource "aws_cloudwatch_metric_alarm" "imageworker_errors" {
  alarm_name          = "${var.project}-imageworker-errors"
  alarm_description   = "画像処理でエラーが出ている。再試行で成功していても、原因は残っている。"
  namespace           = "AWS/Lambda"
  metric_name         = "Errors"
  statistic           = "Sum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"

  dimensions = { FunctionName = aws_lambda_function.imageworker.function_name }

  alarm_actions = local.alarm_actions
  ok_actions    = local.alarm_actions
}

# 通知先。**既定では作らない。**
#
# 送り先を決めずに作っても、鳴っていることに誰も気づかない。
# alert_email を渡したときだけ作り、購読の確認メールが飛ぶ。
resource "aws_sns_topic" "alerts" {
  count = var.alert_email == "" ? 0 : 1
  name  = "${var.project}-alerts"
}

resource "aws_sns_topic_subscription" "alerts_email" {
  count     = var.alert_email == "" ? 0 : 1
  topic_arn = aws_sns_topic.alerts[0].arn
  protocol  = "email"
  endpoint  = var.alert_email
}

locals {
  # 通知先が無ければ空にする。**アラーム自体は作る。**
  # 鳴っている事実はコンソールと API から見える。
  alarm_actions = var.alert_email == "" ? [] : [aws_sns_topic.alerts[0].arn]
}

# 失敗した呼び出しを DLQ へ送る権限。
data "aws_iam_policy_document" "lambda_dlq" {
  statement {
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.imageworker_dlq.arn]
  }
}

resource "aws_iam_role_policy" "lambda_dlq" {
  name   = "dlq"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_dlq.json
}
