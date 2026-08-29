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

resource "aws_ssm_parameter" "jwt_secret" {
  name  = "/${var.project}/JWT_SECRET"
  type  = "SecureString"
  value = random_password.jwt.result

  tags = { Name = "${var.project}-jwt-secret" }
}
