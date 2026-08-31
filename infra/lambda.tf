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
