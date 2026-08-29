terraform {
  required_version = ">= 1.9"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    # データベースのパスワード・JWT の鍵・ALB のオリジン検証ヘッダーを作る。
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # **state はローカルのまま置く。**
  #
  # S3 バックエンドにすると「state を置く S3 を作るのに state が要る」
  # 鶏と卵の問題が出る。今回のスコープは構成を作ることであり、
  # state の置き場を変える作業を同時に持ち込まない
  # （docs/aws-architecture.md 設計判断7）。
  #
  # **state には生成したパスワードと鍵が平文で入る。**
  # infra/.gitignore で除外しているが、扱いには注意する。
}

provider "aws" {
  region = var.region

  # **すべてのリソースに印を付ける。** 手で作ったものと
  # Terraform が作ったものを、コンソール上で見分けられるようにする。
  default_tags {
    tags = {
      Project   = var.project
      ManagedBy = "terraform"
    }
  }
}

# 現在のアカウント ID。IAM のポリシーで条件に使う。
data "aws_caller_identity" "current" {}
