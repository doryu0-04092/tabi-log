########################################
# 秘密の生成と保管
########################################

# **Secrets Manager ではなく Parameter Store を使う。**
# SecureString は無料枠で扱える。秘密の数が少なく自動での入れ替えも要らない
# 学習用途で、1件あたり月額を払う理由がない。

resource "random_password" "db" {
  length = 32
  # RDS のマスターパスワードで使えない文字を除く。
  # 使うと作成そのものが失敗する。
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

# HS256 の共有鍵。**十分な長さを取る。**
# 鍵が短いと、署名の総当たりが現実的な時間で成立する。
resource "random_password" "jwt" {
  length  = 64
  special = false
}

# ALB が「CloudFront から来たこと」を確かめるための合言葉。
#
# **これが実際の防御線である。** セキュリティグループの絞り込みは
# 「CloudFront の IP レンジから来たこと」しか保証せず、
# 他人のディストリビューションも同じレンジから来る。
resource "random_password" "origin_header" {
  length  = 48
  special = false
}

# ---------------------------------------------------------------------------

resource "aws_ssm_parameter" "db_password" {
  name  = "/${var.project}/DB_PASSWORD"
  type  = "SecureString"
  value = random_password.db.result

  # **値そのものは残すが、ログには出さない。**
  # Terraform の出力に平文で出ると、CI のログに残ってしまう。
  tags = { Name = "${var.project}-db-password" }
}

# ---------------------------------------------------------------------------
# 画像配信の署名鍵
# ---------------------------------------------------------------------------

# **CloudFront の公開鍵は RSA 2048 しか受け付けない。**
resource "tls_private_key" "cdn" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "aws_cloudfront_public_key" "cdn" {
  name        = "${var.project}-cdn"
  encoded_key = tls_private_key.cdn.public_key_pem
  comment     = "画像配信の署名付き Cookie を検証する"

  # 鍵を作り直すとき、先に新しい方を作らないとキーグループが空になる。
  lifecycle {
    create_before_destroy = true
  }
}

# **ビヘイビアが参照するのはキーグループであって公開鍵ではない。**
# 鍵を入れ替えるときは、ここに2本入れた状態を挟めば無停止で移せる。
resource "aws_cloudfront_key_group" "cdn" {
  name    = "${var.project}-cdn"
  items   = [aws_cloudfront_public_key.cdn.id]
  comment = "画像配信"
}

resource "aws_ssm_parameter" "cdn_private_key" {
  name  = "/${var.project}/CDN_PRIVATE_KEY"
  type  = "SecureString"
  value = tls_private_key.cdn.private_key_pem

  tags = { Name = "${var.project}-cdn-private-key" }
}

# ---------------------------------------------------------------------------

resource "aws_ssm_parameter" "jwt_secret" {
  name  = "/${var.project}/JWT_SECRET"
  type  = "SecureString"
  value = random_password.jwt.result

  tags = { Name = "${var.project}-jwt-secret" }
}
