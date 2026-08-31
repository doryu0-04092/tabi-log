########################################
# 投稿画像のバケット
########################################

resource "aws_s3_bucket" "images" {
  bucket = var.images_bucket_name

  # **中身があると destroy が失敗する。**
  #
  # S3 の DeleteBucket は空でないバケットを拒否する(409 BucketNotEmpty)。
  # **AWS 側に強制削除のフラグは無い。** force_destroy は「全オブジェクトを
  # 列挙して削除してから DeleteBucket を呼ぶ」というプロバイダの動作を指示する。
  #
  # このプロジェクトは「使わない期間は destroy する」運用を前提にしている。
  # その前提と「中身があると消せない」は両立しない。ECR の force_delete と同じ割り切り。
  #
  # sns-application が実際にこれで詰まった(2026-08-30)。フロントエンドを配置した後の
  # destroy が静的バケットだけ失敗し、後片付けが中途半端に終わった。
  # **使わなければ気づかない種類の欠陥である。**
  #
  # **こちらは利用者の投稿画像が入る。** 静的バケットより重い判断になるが、
  # destroy はバケットごと消す操作であり、中身を残す選択肢は元々無い。
  # 「中身があるときだけ失敗する」は消えないことの保証にはならない。
  #
  # **本番では false にすること。** 消えては困るものが消える。
  force_destroy = true
}

# ACL を無効にし、アクセス制御をバケットポリシーと IAM に一本化する。
resource "aws_s3_bucket_ownership_controls" "images" {
  bucket = aws_s3_bucket.images.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# **完全に非公開にする。**
# 表示は署名付き URL で行うため、公開する必要がない。
resource "aws_s3_bucket_public_access_block" "images" {
  bucket = aws_s3_bucket.images.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "images" {
  bucket = aws_s3_bucket.images.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# **ブラウザから S3 へ直接 PUT するため CORS が要る。**
# これが無いとプリフライトで落ち、アップロードできない。
# 送信元は CloudFront のドメインだけに絞る。
resource "aws_s3_bucket_cors_configuration" "images" {
  bucket = aws_s3_bucket.images.id

  cors_rule {
    allowed_methods = ["PUT"]
    allowed_origins = ["https://${aws_cloudfront_distribution.main.domain_name}"]
    # 署名に焼き込んだ値を送る必要がある。**許可し忘れると
    # ブラウザが preflight で止め、アップロードが一切通らない。**
    # x-amz-tagging は原本の保持印（state=pending）に使う。
    allowed_headers = ["content-type", "x-amz-tagging"]
    expose_headers  = ["ETag"]
    max_age_seconds = 3000
  }
}

# **CloudFront からだけ読ませる。**
#
# 配信するのは variants/ 配下だけである。originals/ にはアップロードされた
# ままの画像があり、**EXIF（GPS 座標を含む）が残っている**。
# EXIF を落とすのは変換の工程であり、その成果物が variants/ である。
data "aws_iam_policy_document" "images_oac" {
  statement {
    sid    = "AllowCloudFrontReadVariants"
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    actions = ["s3:GetObject"]
    # **バケット全体を許さない。** ここを "/*" にすると、
    # 変換前の画像まで配信できる状態になる。
    resources = ["${aws_s3_bucket.images.arn}/variants/*"]

    # この条件が無いと、OAC を設定した他人のディストリビューションからも読める。
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.main.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "images" {
  bucket = aws_s3_bucket.images.id
  policy = data.aws_iam_policy_document.images_oac.json
}

# 確定しなかったアップロードを期限で消す。
#
# 署名付き URL を出したあとブラウザが閉じられると、誰も参照しない
# オブジェクトが残る。`media.status = 'pending'` で記録はしているが、
# **S3 側でも期限を切っておかないと溜まり続ける。**
resource "aws_s3_bucket_lifecycle_configuration" "images" {
  bucket = aws_s3_bucket.images.id

  # 投稿に使われなかった原本だけを消す。
  #
  # **接頭辞だけで絞ってはいけない。** originals/ を無条件に消すと、
  # 投稿に使われた原本まで期限で消え、別解像度を後から作れなくなる
  # (docs/er-diagram.md が前提にしている)。
  #
  # S3 のライフサイクルは「タグが無いこと」を条件にできない。そのため
  # **確定していない側に state=pending を付ける**形にしてある。
  # 付ける役はクライアントで、署名付き URL の署名にタグを焼き込み、
  # 付けずに送ると S3 が弾く。投稿が確定した時点でアプリが kept に変える。
  rule {
    id     = "expire-unconfirmed-originals"
    status = "Enabled"

    # 条件が2つ以上のときは and で包む必要がある。
    filter {
      and {
        prefix = "originals/"
        tags = {
          state = "pending"
        }
      }
    }

    expiration {
      days = var.orphan_expiration_days
    }
  }

  rule {
    id     = "abort-incomplete-multipart-uploads"
    status = "Enabled"

    filter {}

    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

# アップロードされたら画像処理 Lambda を起こす。
#
# **接頭辞を originals/ に絞る。** 絞らないと、Lambda 自身が書き出す
# 変換後の画像でもう一度 Lambda が起動し、際限なく回る。
resource "aws_s3_bucket_notification" "images" {
  bucket = aws_s3_bucket.images.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.imageworker.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "originals/"
  }

  # 許可より先に通知を作ると、S3 が「呼べない」と判断して作成に失敗する。
  depends_on = [aws_lambda_permission.from_s3]
}

########################################
# 静的サイトのバケット
########################################

resource "aws_s3_bucket" "static" {
  bucket = var.static_bucket_name

  # **中身があると destroy が失敗する。**
  #
  # S3 の DeleteBucket は空でないバケットを拒否する(409 BucketNotEmpty)。
  # **AWS 側に強制削除のフラグは無い。** force_destroy は「全オブジェクトを
  # 列挙して削除してから DeleteBucket を呼ぶ」というプロバイダの動作を指示する。
  #
  # このプロジェクトは「使わない期間は destroy する」運用を前提にしている。
  # その前提と「中身があると消せない」は両立しない。ECR の force_delete と同じ割り切り。
  #
  # sns-application が実際にこれで詰まった(2026-08-30)。フロントエンドを配置した後の
  # destroy が静的バケットだけ失敗し、後片付けが中途半端に終わった。
  # **使わなければ気づかない種類の欠陥である。**
  #
  # ここに入るのはビルド成果物だけで、失っても再ビルドで復元できる。
  #
  # **本番では false にすること。** 消えては困るものが消える。
  force_destroy = true
}

resource "aws_s3_bucket_ownership_controls" "static" {
  bucket = aws_s3_bucket.static.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# **静的サイトでも公開しない。** CloudFront の OAC からのみ読ませる。
# バケットを直接公開すると、CloudFront を迂回されて
# キャッシュもヘッダーも効かない経路ができる。
resource "aws_s3_bucket_public_access_block" "static" {
  bucket = aws_s3_bucket.static.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "static" {
  bucket = aws_s3_bucket.static.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

data "aws_iam_policy_document" "static_oac" {
  statement {
    sid    = "AllowCloudFrontRead"
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.static.arn}/*"]

    # **この条件が無いと、OAC を設定した他人のディストリビューションからも読める。**
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.main.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "static" {
  bucket = aws_s3_bucket.static.id
  policy = data.aws_iam_policy_document.static_oac.json
}
