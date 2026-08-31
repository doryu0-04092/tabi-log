########################################
# CD（GitHub Actions からのデプロイ）
########################################

# **アクセスキーは発行しない。** GitHub の OIDC トークンを AWS に信頼させ、
# 実行のたびに一時的な認証情報を受け取る。長期キーをリポジトリの秘密に置くと、
# 漏れた時に失効させるまで有効なままで、ローテーションの手間も残り続ける。
#
# **このロールが行えるのはアプリのデプロイだけである。**
# インフラの変更（terraform apply）は含めていない。state をローカルで管理しており
# （versions.tf）、CI から共有できないためである。

########################################
# OIDC プロバイダ
########################################

# **アカウントに1つしか作れない。**
# 同じ AWS アカウントで別のリポジトリが既に作っている場合、ここで作ろうとすると
# EntityAlreadyExists で失敗する。その場合は github_oidc_provider_arn に
# 既存の ARN を渡すこと。
#
# このアカウントでは sns-application も同じものを作る。**同時にデプロイする場合は
# 片方が既存の ARN を受け取る形にする。**
resource "aws_iam_openid_connect_provider" "github" {
  count = var.github_oidc_provider_arn == "" ? 1 : 0

  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]

  # thumbprint_list は指定しない。
  # この issuer について AWS は自前の信頼ストアで検証しており、値を渡しても使われない。
  # プロバイダのスキーマ上も optional かつ computed である。
  # **固定値を書くと、GitHub 側の証明書が変わった時に更新漏れの原因になるだけ。**
}

locals {
  github_oidc_provider_arn = var.github_oidc_provider_arn != "" ? var.github_oidc_provider_arn : aws_iam_openid_connect_provider.github[0].arn
}

########################################
# デプロイ用ロール
########################################

data "aws_iam_policy_document" "github_actions_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [local.github_oidc_provider_arn]
    }

    # **aud の確認は省略できない。**
    # 省くと、他の OIDC 利用者が発行させたトークンでも通りうる形になる。
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    # **ここが最も重要。** sub を絞らないと、GitHub 上の**どのリポジトリからでも**
    # このロールを引き受けられる。OIDC の設定で最も多い致命的な誤りがこれである。
    #
    # **所有者名とリポジトリ名だけでは一致しない。** GitHub が発行する sub には
    # ID が埋め込まれている（repo:<所有者>@<ID>/<名前>@<ID>:ref:...）。
    # 2026-08-31 に CD の初回実行がこれで落ちた。詳細は variables.tf を見ること。
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = var.github_deploy_subjects
    }
  }
}

resource "aws_iam_role" "github_actions" {
  name               = "${var.project}-github-actions-deploy"
  assume_role_policy = data.aws_iam_policy_document.github_actions_assume_role.json

  # **description は英語で書く。IAM だけ日本語を受け付けない。**
  #
  # IAM は description を ASCII 印字可能文字と Latin-1 補助の範囲に制限しており、
  # 日本語を入れると CreateRole が ValidationError で失敗する。
  # sns-application が実際に踏んだ。
  #
  # **これは EC2 と IAM 固有の制約である。** CloudFront・ECR・SSM は
  # 日本語のままで作成できる。一般化しないこと。
  #
  # 日本語の説明:
  #   GitHub Actions からアプリをデプロイするためのロール。
  #   インフラの変更権限は持たない（state をローカル管理しているため）。
  description = "Deploys the application from GitHub Actions. Cannot change infrastructure."
}

########################################
# 権限（デプロイに必要な分だけ）
########################################

data "aws_iam_policy_document" "github_actions_deploy" {
  # --- ECR: イメージの push ---
  # GetAuthorizationToken だけは特定のリポジトリに絞れない
  # （トークンはレジストリ単位のため）。
  statement {
    sid       = "EcrLogin"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid = "EcrPush"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:CompleteLayerUpload",
      "ecr:InitiateLayerUpload",
      "ecr:PutImage",
      "ecr:UploadLayerPart",
      "ecr:BatchGetImage",
      "ecr:DescribeImages",
    ]
    resources = [aws_ecr_repository.backend.arn]
  }

  # --- ECS: 新しいタスク定義を登録し、サービスを入れ替える ---
  # RegisterTaskDefinition と DescribeTaskDefinition はリソース単位で絞れない
  # （登録前のタスク定義に ARN が無いため）。AWS 側の制約であり、書き方の問題ではない。
  statement {
    sid = "EcsRegisterTaskDefinition"
    actions = [
      "ecs:RegisterTaskDefinition",
      "ecs:DescribeTaskDefinition",
    ]
    resources = ["*"]
  }

  statement {
    sid = "EcsDeploy"
    actions = [
      "ecs:UpdateService",
      "ecs:DescribeServices",
    ]
    resources = [aws_ecs_service.backend.id]
  }

  # --- マイグレーションを流す ---
  #
  # **tabi-log は起動時に自動適用しない**（スキーマの変更をデプロイから
  # 切り離すため）。そのため CD が明示的に流せる必要がある。
  # RunTask は「どのタスク定義でも起動できる」状態にしない。
  statement {
    sid     = "EcsRunMigrate"
    actions = ["ecs:RunTask"]
    # **リビジョン番号を落として :* にする。** ARN をそのまま書くと、
    # 移行を1つ足してリビジョンが上がった瞬間に権限から外れる。
    #
    # **正規表現に d は使えない。** HCL の文字列として不正なエスケープになる
    # (terraform console で確認: The symbol d is not a valid escape sequence selector)。
    # [0-9] なら通ることを確認済み。**apply して初めて分かる誤りだった。**
    resources = ["${replace(aws_ecs_task_definition.migrate.arn, "/:[0-9]+$/", "")}:*"]

    condition {
      test     = "ArnEquals"
      variable = "ecs:cluster"
      values   = [aws_ecs_cluster.main.arn]
    }
  }

  statement {
    sid       = "EcsDescribeTasks"
    actions   = ["ecs:DescribeTasks"]
    resources = ["*"]

    condition {
      test     = "ArnEquals"
      variable = "ecs:cluster"
      values   = [aws_ecs_cluster.main.arn]
    }
  }

  # --- タスク定義に載せるロールを渡す権限 ---
  #
  # **これが無いと RegisterTaskDefinition も RunTask も失敗する。**
  # 渡せる相手を2つのロールに限定し、さらに渡し先のサービスを ECS に限定する。
  # 絞らないと、**任意のロールを ECS タスクとして起動できる**ことになり、
  # 権限昇格の経路になる。
  statement {
    sid     = "PassTaskRoles"
    actions = ["iam:PassRole"]
    resources = [
      aws_iam_role.execution.arn,
      aws_iam_role.task.arn,
    ]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["ecs-tasks.amazonaws.com"]
    }
  }

  # --- 移行の結果を読む ---
  statement {
    sid     = "ReadMigrateLogs"
    actions = ["logs:DescribeLogStreams", "logs:GetLogEvents"]
    # **末尾の :* を一度落としてから付け直す。**
    # aws_cloudwatch_log_group.arn は :* を含む形と含まない形があり、
    # そのまま連結すると :*:* になって権限が一致しない。
    # trimsuffix ならどちらの形でも同じ結果になる(確認済み)。
    resources = ["${trimsuffix(aws_cloudwatch_log_group.migrate.arn, ":*")}:*"]
  }

  # --- S3: フロントエンドの配置 ---
  #
  # --delete を使うため DeleteObject が要る。対象は静的配信バケットのみ。
  # **画像バケットは含めない。** 利用者の投稿画像であり、デプロイで消えてよいものではない。
  statement {
    sid       = "StaticBucketList"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.static.arn]
  }

  statement {
    sid = "StaticBucketWrite"
    actions = [
      "s3:PutObject",
      "s3:GetObject",
      "s3:DeleteObject",
    ]
    resources = ["${aws_s3_bucket.static.arn}/*"]
  }

  # --- CloudFront: index.html のキャッシュ無効化 ---
  statement {
    sid = "CloudFrontInvalidate"
    actions = [
      "cloudfront:CreateInvalidation",
      "cloudfront:GetInvalidation",
    ]
    resources = [aws_cloudfront_distribution.main.arn]
  }
}

resource "aws_iam_role_policy" "github_actions_deploy" {
  name   = "${var.project}-github-actions-deploy"
  role   = aws_iam_role.github_actions.id
  policy = data.aws_iam_policy_document.github_actions_deploy.json
}
