########################################
# 投稿画像のバケット
########################################

resource "aws_s3_bucket" "images" {
  bucket = var.images_bucket_name
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
    # 署名に焼き込んだ Content-Type を送る必要がある。
    allowed_headers = ["content-type"]
    expose_headers  = ["ETag"]
    max_age_seconds = 3000
  }
}

# 確定しなかったアップロードを期限で消す。
#
# 署名付き URL を出したあとブラウザが閉じられると、誰も参照しない
# オブジェクトが残る。`media.status = 'pending'` で記録はしているが、
# **S3 側でも期限を切っておかないと溜まり続ける。**
resource "aws_s3_bucket_lifecycle_configuration" "images" {
  bucket = aws_s3_bucket.images.id

  rule {
    id     = "expire-unconfirmed-originals"
    status = "Enabled"

    filter {
      prefix = "originals/"
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
