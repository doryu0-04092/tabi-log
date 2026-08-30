########################################
# VPC とサブネット
########################################

# 使えるアベイラビリティゾーン。**名前を書き込まない。**
# リージョンを変えたときに、そのリージョンに無いゾーン名が残るのを避ける。
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "main" {
  cidr_block = var.vpc_cidr

  # RDS のエンドポイント名を解決するために両方とも必要である。
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.project }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = var.project }
}

# ---------------------------------------------------------------------------
# パブリックサブネット（ALB と Fargate）
# ---------------------------------------------------------------------------

# **Fargate をここに置いているのは費用の判断である。**
# プライベートに置くと、ECR からイメージを取るためと SSM から秘密を取るために
# NAT ゲートウェイ（月$35前後の常時課金）か複数の VPC エンドポイントが要る。
#
# パブリック IP は付くが、セキュリティグループの受信を ALB からの :8080 だけに
# 絞るため外からは届かない。**この妥協はアプリケーション層に限る。**
# データベースは IGW へのルートを持たないサブネットに置く。
resource "aws_subnet" "public" {
  count = length(var.public_subnet_cidrs)

  vpc_id            = aws_vpc.main.id
  cidr_block        = var.public_subnet_cidrs[count.index]
  availability_zone = data.aws_availability_zones.available.names[count.index]

  # Fargate のタスクが ECR と SSM へ出るために要る。
  map_public_ip_on_launch = true

  tags = { Name = "${var.project}-public-${count.index}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = { Name = "${var.project}-public" }
}

resource "aws_route_table_association" "public" {
  count = length(aws_subnet.public)

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# ---------------------------------------------------------------------------
# プライベートサブネット（RDS と画像処理 Lambda）
# ---------------------------------------------------------------------------

resource "aws_subnet" "private" {
  count = length(var.private_subnet_cidrs)

  vpc_id            = aws_vpc.main.id
  cidr_block        = var.private_subnet_cidrs[count.index]
  availability_zone = data.aws_availability_zones.available.names[count.index]

  tags = { Name = "${var.project}-private-${count.index}" }
}

# **外向きのルートを持たせない。** ここに置いたものは
# インターネットへ出られないし、インターネットからも届かない。
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.project}-private" }
}

resource "aws_route_table_association" "private" {
  count = length(aws_subnet.private)

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

# ---------------------------------------------------------------------------
# S3 のゲートウェイエンドポイント
# ---------------------------------------------------------------------------

# **画像処理 Lambda が S3 へ届くために必要である。**
#
# Lambda はデータベースを更新するため VPC の中に置く。しかしプライベート
# サブネットには NAT が無いので、そのままでは S3 に到達できず、
# **画像を読めないまま毎回タイムアウトする**（設計書の構成図には
# Lambda → S3 の線があるが、この経路の手当ては書かれていなかった）。
#
# ゲートウェイ型のエンドポイントは**無料**で、ルートテーブルに経路を足すだけである。
# インターフェース型（有料・時間課金）を使う必要はない。
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "${var.project}-s3" }
}

########################################
# セキュリティグループ
########################################

# ALB。**CloudFront からの :80 だけを通す。**
#
# ただしこれは「CloudFront から来たこと」しか保証しない。他人が作った
# ディストリビューションも同じ IP レンジから来る。
# **実際の防御線はリスナールールのヘッダー検証**である（alb.tf）。
# **security group の description は英語で書く。**
#
# EC2 API は description に非ASCIIを受け付けない(SG本体もルールも)。
# 日本語を入れると apply が ValidationError で失敗する。
# sns-application が同じ形で踏んでおり、あちらは英語に直して通っている。
#
# **terraform validate では検出できない。** Terraform の構文としては正しく、
# AWS API 側の制約であるため。apply して初めて分かる。
#
# **これは EC2 と IAM 固有の制約である。** CloudFront・ECR・SSM は
# 日本語のままで作成できる(sns-application で実績あり)。一般化しないこと。
#
# 日本語の説明は各 description の直上にコメントとして残してある。
resource "aws_security_group" "alb" {
  name = "${var.project}-alb"
  # CloudFront から ALB への受信のみ許可する
  description = "ALB: accepts traffic only from CloudFront edge locations"
  vpc_id      = aws_vpc.main.id

  tags = { Name = "${var.project}-alb" }
}

# CloudFront のエッジが使う IP レンジ。**自分で列挙しない。**
# AWS が管理するリストを参照すれば、レンジが増えても追随できる。
data "aws_ec2_managed_prefix_list" "cloudfront" {
  name = "com.amazonaws.global.cloudfront.origin-facing"
}

resource "aws_vpc_security_group_ingress_rule" "alb_from_cloudfront" {
  security_group_id = aws_security_group.alb.id
  # CloudFront のエッジからのみ
  description = "From CloudFront edge locations only"

  prefix_list_id = data.aws_ec2_managed_prefix_list.cloudfront.id
  from_port      = 80
  to_port        = 80
  ip_protocol    = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "alb_to_tasks" {
  security_group_id = aws_security_group.alb.id
  # ターゲットのタスクへ
  description = "To the target ECS tasks"

  referenced_security_group_id = aws_security_group.tasks.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

# ---------------------------------------------------------------------------

# ECS タスク。**受信は ALB からだけ。**
# パブリックサブネットに置いてパブリック IP が付くが、ここで閉じる。
resource "aws_security_group" "tasks" {
  name = "${var.project}-tasks"
  # ALB からの受信のみ許可し、送信は制限しない
  description = "ECS tasks: accepts traffic only from the ALB, unrestricted egress"
  vpc_id      = aws_vpc.main.id

  tags = { Name = "${var.project}-tasks" }
}

resource "aws_vpc_security_group_ingress_rule" "tasks_from_alb" {
  security_group_id = aws_security_group.tasks.id
  # ALB からのみ
  description = "From the ALB only"

  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

# 送信は絞らない。ECR・SSM・S3・CloudWatch Logs へ出る必要があり、
# それぞれの宛先を IP で列挙するのは維持できない。
resource "aws_vpc_security_group_egress_rule" "tasks_all" {
  security_group_id = aws_security_group.tasks.id
  # ECR・SSM・S3・CloudWatch Logs へ
  description = "Outbound to ECR / SSM / S3 / CloudWatch Logs"

  cidr_ipv4   = "0.0.0.0/0"
  ip_protocol = "-1"
}

# ---------------------------------------------------------------------------

# 画像処理 Lambda。データベースと S3 に届けばよい。
resource "aws_security_group" "lambda" {
  name = "${var.project}-lambda"
  # 画像処理 Lambda。受信は無し
  description = "Image worker Lambda: no ingress"
  vpc_id      = aws_vpc.main.id

  tags = { Name = "${var.project}-lambda" }
}

resource "aws_vpc_security_group_egress_rule" "lambda_all" {
  security_group_id = aws_security_group.lambda.id
  # RDS と S3（ゲートウェイエンドポイント経由）へ
  description = "Outbound to RDS and S3 via the gateway endpoint"

  cidr_ipv4   = "0.0.0.0/0"
  ip_protocol = "-1"
}

# ---------------------------------------------------------------------------

# RDS。**受信はタスクと Lambda からの :3306 だけ。**
# インターネットからは、サブネットにルートが無い時点で届かない。
resource "aws_security_group" "db" {
  name = "${var.project}-db"
  # ECS タスクと画像処理 Lambda からの :3306 のみ
  description = "RDS: accepts 3306 from ECS tasks and the image worker Lambda only"
  vpc_id      = aws_vpc.main.id

  tags = { Name = "${var.project}-db" }
}

resource "aws_vpc_security_group_ingress_rule" "db_from_tasks" {
  security_group_id = aws_security_group.db.id
  # ECS タスクから
  description = "From the ECS tasks"

  referenced_security_group_id = aws_security_group.tasks.id
  from_port                    = 3306
  to_port                      = 3306
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "db_from_lambda" {
  security_group_id = aws_security_group.db.id
  # 画像処理 Lambda から
  description = "From the image worker Lambda"

  referenced_security_group_id = aws_security_group.lambda.id
  from_port                    = 3306
  to_port                      = 3306
  ip_protocol                  = "tcp"
}
