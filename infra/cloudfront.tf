########################################
# CloudFront
########################################

# **窓口を1つにする。** 静的ファイルも API も同じドメインから配る。
# ブラウザから見て同一オリジンになるため、
#
#   - CORS の設定が実質要らない
#   - リフレッシュトークンの Cookie が SameSite=Strict のまま素直に飛ぶ
#   - 独自ドメインと証明書を用意せずに済む
#
# 分離する（API は ALB を直接叩く）と SameSite=None; Secure が必須になり、
# ALB 用のドメインと証明書も要る。

# S3 を非公開のまま読ませるための署名。
resource "aws_cloudfront_origin_access_control" "static" {
  name                              = "${var.project}-static"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# ---------------------------------------------------------------------------
# キャッシュとリクエストのポリシー
# ---------------------------------------------------------------------------

data "aws_cloudfront_cache_policy" "optimized" {
  name = "Managed-CachingOptimized"
}

data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

# API へは、あらゆるヘッダー・Cookie・クエリをそのまま渡す必要がある。
data "aws_cloudfront_origin_request_policy" "all_viewer" {
  name = "Managed-AllViewer"
}

# ---------------------------------------------------------------------------

resource "aws_cloudfront_distribution" "main" {
  enabled = true
  comment = var.project

  # SPA の入口。CloudFront が index.html を返す。
  default_root_object = "index.html"

  # **価格クラスを絞る。** 全世界のエッジを使う必要は無く、
  # 日本からの利用を想定している。
  price_class = "PriceClass_200"

  # --- オリジン: 静的サイト（S3） --------------------------------------
  origin {
    origin_id                = "static"
    domain_name              = aws_s3_bucket.static.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.static.id
  }

  # --- オリジン: API（ALB） --------------------------------------------
  origin {
    origin_id   = "api"
    domain_name = aws_lb.main.dns_name

    custom_origin_config {
      http_port  = 80
      https_port = 443
      # **CloudFront と ALB の間は HTTP である。**
      # 証明書を持たない ALB に HTTPS では話せない。
      # 利用者との間（CloudFront まで）は常に HTTPS である。
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }

    # **合言葉。これが ALB を守る実際の線である。**
    # セキュリティグループは「CloudFront の IP レンジから来たこと」しか
    # 保証しない（他人のディストリビューションも同じレンジから来る）。
    custom_header {
      name  = "X-Origin-Verify"
      value = random_password.origin_header.result
    }
  }

  # --- 既定の振る舞い: 静的サイト ---------------------------------------
  default_cache_behavior {
    target_origin_id       = "static"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]

    # Vite の出力はファイル名にハッシュが付くため、長く持たせて安全である。
    cache_policy_id = data.aws_cloudfront_cache_policy.optimized.id

    compress = true
  }

  # --- /api/*: ALB へ ---------------------------------------------------
  ordered_cache_behavior {
    path_pattern           = "/api/*"
    target_origin_id       = "api"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods         = ["GET", "HEAD"]

    # **キャッシュしない。これは性能上の妥協ではなく安全性の判断である。**
    #
    # この API はアクセストークンで認証しており、応答は閲覧者ごとに違う
    # （フィードの isLiked、フォロー中フィード、通知の未読数）。
    # キャッシュすると、ある利用者向けの応答が別の利用者に配られる。
    #
    # 利用者ごとにキャッシュキーを分ける方法もあるが、
    # ヒット率がほぼ出ないうえ、設定を誤ったときの被害が大きい。
    # 転送量は CDN ではなく gzip で減らす。
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer.id

    compress = true
  }

  # --- SPA のディープリンク ---------------------------------------------
  #
  # /posts/123 のような URL は S3 に存在しない。403 が返るため、
  # index.html を返して画面側のルーターに処理させる。
  #
  # **404 ではなく 403 を拾うのは、バケットを非公開にしているためである。**
  # 非公開のバケットは「無い」ではなく「読めない」を返す。
  custom_error_response {
    error_code            = 403
    response_code         = 200
    response_page_path    = "/index.html"
    error_caching_min_ttl = 0
  }

  custom_error_response {
    error_code            = 404
    response_code         = 200
    response_page_path    = "/index.html"
    error_caching_min_ttl = 0
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    # 既定のドメイン（dxxxx.cloudfront.net）を使う。
    # 独自ドメインにするには ACM の証明書（us-east-1）と Route53 が要る。
    cloudfront_default_certificate = true
  }

  tags = { Name = var.project }
}

# ---------------------------------------------------------------------------
# 画像の配信について
# ---------------------------------------------------------------------------

# **/images/* の振る舞いは作っていない。**
#
# 設計書（docs/aws-architecture.md）には CloudFront の署名付き Cookie で
# 画像を配る構成が書いてあるが、**アプリケーションは現在 S3 の署名付き URL を
# 返している**（internal/storage/s3.go の PresignGet）。
# 画面は S3 のドメインを直接指すため、CloudFront の経路を作っても
# 誰も通らない。**使われない設定を置くと、動いているつもりの構成が残る。**
#
# 署名付き Cookie に移すなら、必要なのは Terraform だけではない。
#
#   1. アプリケーション: PresignGet を CloudFront の URL 生成に差し替え、
#      ログイン時に署名付き Cookie を発行する
#   2. Terraform: 画像バケットへの OAC、公開鍵とキーグループ、
#      /images/* の振る舞い、CDN_PRIVATE_KEY の Parameter Store 登録
#
# **どちらが良いかは配信量で決まる。** 署名付き URL は毎回 S3 から出るため
# 転送量がそのまま課金になる。CloudFront を通せばエッジで効く。
# 現時点では利用者がいないので、先に作る理由が無い。
