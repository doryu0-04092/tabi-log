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

resource "aws_cloudfront_origin_access_control" "images" {
  name                              = "${var.project}-images"
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

# 画像用。CachingOptimized とほぼ同じだが、**圧縮済みオブジェクトの
# キャッシュ設定が無効**である点だけ違う。
#
# CachingOptimized は正規化した Accept-Encoding をキャッシュキーに含めるため、
# **JPEG のように再圧縮しても縮まないものでも、gzip / br / 無圧縮で
# キャッシュが分かれる。** 画像には不利なので、そこを含めない方を使う。
data "aws_cloudfront_cache_policy" "optimized_uncompressed" {
  name = "Managed-CachingOptimizedForUncompressedObjects"
}

# API へは、あらゆるヘッダー・Cookie・クエリをそのまま渡す必要がある。
data "aws_cloudfront_origin_request_policy" "all_viewer" {
  name = "Managed-AllViewer"
}

# ---------------------------------------------------------------------------

# 拡張子を持たない URI を /index.html に書き換える。
#
# **静的配信のビヘイビアにだけ付ける。** ここが custom_error_response との違いで、
# /api/* と /variants/* のエラーには一切触れない。
#
# 判定は「最後のセグメントにドットがあるか」。
# /discover や /posts/1 は書き換え、/_app/immutable/xxx.js はそのまま通す。
resource "aws_cloudfront_function" "spa_fallback" {
  name    = "${var.project}-spa-fallback"
  runtime = "cloudfront-js-2.0"
  comment = "拡張子を持たない URI を /index.html に書き換える(SPA のディープリンク対応)"
  publish = true

  code = <<-JS
    function handler(event) {
      var request = event.request;
      var uri = request.uri;

      var lastSegment = uri.substring(uri.lastIndexOf("/") + 1);
      if (lastSegment === "" || lastSegment.indexOf(".") === -1) {
        request.uri = "/index.html";
      }

      return request;
    }
  JS
}
# 応答ヘッダー（安全側の既定）
#
# **全経路に付ける。** 画面も API も画像も同じ配信元から出ており、
# 経路ごとに抜けがあると、抜けている経路が狙われる。
resource "aws_cloudfront_response_headers_policy" "security" {
  name = "${var.project}-security"

  security_headers_config {
    # HTTPS 以外で来させない。**CloudFront は HTTP を HTTPS に
    # 転送しているが、その1回目は平文で飛ぶ。** HSTS があれば
    # 2回目以降はブラウザが自分で HTTPS にする。
    #
    # preload は申請しない。**取り消しに数か月かかる**ため、
    # 独自ドメインを持たない学習用の構成では割に合わない。
    strict_transport_security {
      access_control_max_age_sec = 31536000
      include_subdomains         = true
      preload                    = false
      override                   = true
    }

    # 宣言した型として扱わせる。**画像として上げたものが
    # スクリプトとして解釈される経路を塞ぐ。**
    content_type_options {
      override = true
    }

    # 埋め込ませない。クリックジャッキング対策。
    frame_options {
      frame_option = "DENY"
      override     = true
    }

    # 外部サイトへ遷移するときに、どのページから来たかを渡さない。
    # **投稿の URL には投稿 ID が入る。**
    referrer_policy {
      referrer_policy = "strict-origin-when-cross-origin"
      override        = true
    }

    # **script-src を self に絞るのが要点である。** 外部の CDN から
    # スクリプトを読めなくする。読めると、その CDN が改ざんされた時点で
    # 同一オリジンから API を呼ばれる（docs/audit-2026-08-31.md H4）。
    #
    # unsafe-inline を許しているのは、SvelteKit が起動用のスクリプトを
    # HTML に直接書き込むためである。**外すには SvelteKit 側の csp 設定で
    # ハッシュを埋める必要があり、それは別の変更として行う。**
    # 外部スクリプトを塞ぐという主目的はこのままでも果たせている。
    #
    # connect-src に画像バケットを入れているのは、**アップロードが
    # ブラウザから S3 へ直接 PUT する**ためである。入れ忘れると
    # 画像の送信だけが動かなくなる。
    content_security_policy {
      content_security_policy = join("; ", [
        "default-src 'self'",
        "script-src 'self' 'unsafe-inline'",
        "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
        "font-src 'self' https://fonts.gstatic.com",
        "img-src 'self' data: blob:",
        "connect-src 'self' https://${aws_s3_bucket.images.bucket_regional_domain_name}",
        "frame-ancestors 'none'",
        "base-uri 'self'",
        "form-action 'self'",
        "object-src 'none'",
      ])
      override = true
    }
  }
}

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

  # --- オリジン: 画像（S3） ---------------------------------------------
  origin {
    origin_id                = "images"
    domain_name              = aws_s3_bucket.images.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.images.id
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
    cache_policy_id            = data.aws_cloudfront_cache_policy.optimized.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id

    compress = true

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.spa_fallback.arn
    }
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
    cache_policy_id            = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id

    compress = true
  }

  # --- /variants/*: 投稿画像 --------------------------------------------
  #
  # **ここがこの構成の主目的である。**
  #
  # 以前は S3 の署名付き URL を返していた。署名付き URL は呼ぶたびに
  # 署名と時刻が変わるため、**同じ画像でも毎回別の URL** になり、
  # エッジのキャッシュもブラウザのキャッシュも一度も当たらなかった。
  # 画像が主役のサービスで、転送量の9割以上を占める経路である。
  #
  # URL を /variants/<鍵> に固定し、**読む権利は署名付き Cookie に移した。**
  # URL が変わらないので、エッジにもブラウザにも載る。
  ordered_cache_behavior {
    path_pattern           = "/variants/*"
    target_origin_id       = "images"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]

    # 変換物は一度作られたら中身が変わらない。長く持たせて安全である。
    #
    # **画像用のポリシーを使う。** CachingOptimized だと
    # Accept-Encoding がキャッシュキーに入り、再圧縮しても縮まない
    # JPEG が gzip / br / 無圧縮で分かれてしまう。
    cache_policy_id            = data.aws_cloudfront_cache_policy.optimized_uncompressed.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id

    # **Cookie を持つ者だけに配る。**
    # これが無いと、URL を知っている全員が読めることになる。
    #
    # **署名付き Cookie はキャッシュキーに入らない**（上のポリシーは
    # Cookie を含めない）。CloudFront はエッジで検証してから
    # キャッシュを引くので、利用者ごとにキャッシュが分裂しない。
    trusted_key_groups = [aws_cloudfront_key_group.cdn.id]

    # 画像は既に圧縮済みで、再圧縮しても縮まない。
    compress = false
  }

  # --- SPA のディープリンク ---------------------------------------------
  #
  # /posts/123 のような URL は S3 に存在しない。403 が返るため、
  # index.html を返して画面側のルーターに処理させる。
  #
  # **404 ではなく 403 を拾うのは、バケットを非公開にしているためである。**
  # 非公開のバケットは「無い」ではなく「読めない」を返す。
  # **custom_error_response は使わない。**
  #
  # SPA のディープリンク対応として 403/404 を /index.html に差し替える書き方があるが、
  # **この設定はディストリビューション全体に効き、ビヘイビアごとに限定できない。**
  # そのため /api/* の 403 と 404 まで 200 + HTML に化ける。
  #
  # 実際にデプロイして確認した(2026-08-30):
  #
  #   他人の画像を見る  仕様 403 JSON  ->  実際 200 text/html
  #   存在しない経路    仕様 404 JSON  ->  実際 200 text/html
  #   未認証            仕様 401 JSON  ->  実際 401 JSON（401は対象外なので無事）
  #
  # **ローカルでは絶対に出ない。** CloudFront が無いためである。
  # 代わりに、静的配信のビヘイビアにだけ Function を付けて書き換える。

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

# **アップロードは CloudFront を通らない。**
#
# ブラウザは S3 の署名付き URL へ直接 PUT する（バックエンドの帯域と
# タイムアウトを消費しないため）。CloudFront は読む側だけを担う。
# 画像バケットの CORS が PUT だけを許しているのはそのためである。
#
# **originals/ は CloudFront から読めない。**
#
# ビヘイビアが /variants/* しか無く、バケットポリシーも variants/ 配下しか
# 許していない。二重に絞っているのは、**変換前の画像に EXIF（GPS 座標）が
# 残っている**ためである。片方だけでは、もう片方を緩めたときに気づけない。
