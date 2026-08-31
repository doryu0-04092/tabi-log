variable "project" {
  description = <<-EOT
    リソース名の接頭辞。

    **ALB とターゲットグループの名前には32文字の上限がある。**
    接頭辞を長くする場合は、それらの名前が上限に収まるかを確認すること。
  EOT
  type        = string
  default     = "tabilog"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.project))
    error_message = "接頭辞は小文字英数字とハイフンで2〜21文字にしてください（ALB 名の32文字上限に収めるため）。"
  }
}

variable "region" {
  description = "リソースを作るリージョン"
  type        = string
  default     = "ap-northeast-1"
}

# ---------------------------------------------------------------------------
# ネットワーク
# ---------------------------------------------------------------------------

variable "vpc_cidr" {
  description = "VPC の CIDR"
  type        = string
  default     = "10.0.0.0/16"
}

variable "public_subnet_cidrs" {
  description = <<-EOT
    パブリックサブネットの CIDR。**2つ以上必要である。**
    ALB は2つ以上のアベイラビリティゾーンにまたがることを要求する。
  EOT
  type        = list(string)
  default     = ["10.0.0.0/24", "10.0.1.0/24"]

  validation {
    condition     = length(var.public_subnet_cidrs) >= 2
    error_message = "ALB が2AZ以上を要求するため、2つ以上指定してください。"
  }
}

variable "private_subnet_cidrs" {
  description = <<-EOT
    プライベートサブネットの CIDR。**2つ以上必要である。**
    RDS のサブネットグループが2AZ以上を要求する。
  EOT
  type        = list(string)
  default     = ["10.0.10.0/24", "10.0.11.0/24"]

  validation {
    condition     = length(var.private_subnet_cidrs) >= 2
    error_message = "RDS のサブネットグループが2AZ以上を要求するため、2つ以上指定してください。"
  }
}

# ---------------------------------------------------------------------------
# ストレージ
# ---------------------------------------------------------------------------

variable "images_bucket_name" {
  description = <<-EOT
    投稿画像を置くバケット名。

    **S3 のバケット名は全 AWS アカウントで一意である。**
    他と衝突しない名前を terraform.tfvars で指定すること
    （例: tabilog-media-<自分の識別子>）。
  EOT
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.images_bucket_name))
    error_message = "バケット名は3〜63文字の小文字英数字・ハイフン・ピリオドで指定してください。"
  }
}

variable "static_bucket_name" {
  description = <<-EOT
    フロントエンドの成果物（frontend/build）を置くバケット名。
    画像用とは別のバケットにする。こちらも全アカウントで一意。
  EOT
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.static_bucket_name))
    error_message = "バケット名は3〜63文字の小文字英数字・ハイフン・ピリオドで指定してください。"
  }
}

variable "orphan_expiration_days" {
  description = <<-EOT
    確定しなかったアップロードを消すまでの日数。

    署名付き URL を発行したあとブラウザが閉じられると、誰も参照しない
    オブジェクトが残る。`media.status = 'pending'` で記録はしているが、
    **S3 側でも期限を切っておかないと溜まり続ける**
    （docs/aws-architecture.md 設計判断6）。
  EOT
  type        = number
  default     = 7
}

# ---------------------------------------------------------------------------
# ECS
# ---------------------------------------------------------------------------

variable "task_cpu" {
  description = <<-EOT
    タスクの CPU（1024 = 1 vCPU）。

    **既定は 256（1/4コア）。動作確認のための最小構成である。**

    想定ピーク（50 req/s）を捌く構成は 512（0.5 vCPU）× 2タスクだが、
    **常時動かさない前提**なので、確認用にはここまで落とす。
    課題の資料でも「作って動作確認したら即消すくらいの温度感でよい」
    とされている。

    **負荷を測るときは 512 に戻すこと。** 256 で測った数字は
    想定構成の性能を表さない。
  EOT
  type        = number
  default     = 256
}

variable "task_memory" {
  description = <<-EOT
    タスクのメモリ（MiB）。

    **Fargate は CPU との組み合わせが決まっている。**
    256 に対して選べるのは 512 / 1024 / 2048 のみ。
  EOT
  type        = number
  default     = 512
}

variable "desired_count" {
  description = <<-EOT
    動かすタスクの数。

    **既定は 1。動作確認のための最小構成である。**

    **1では、デプロイ中と障害時に止まる。** 冗長性が要るなら 2 にする
    （タスクが1つ落ちても残りが受ける）。常時動かさない前提なので、
    確認用には 1 で足りる。

    自動増減（Application Auto Scaling）は入れていない。
    入れる場合、増やす判断の指標は ECS タスクの CPU ではなく
    **RDS の CPU** が適切である（先に詰まるのはデータベース側のため）。
  EOT
  type        = number
  default     = 1
}

variable "image_tag" {
  description = <<-EOT
    ECR から取るイメージのタグ。

    **latest を既定にしない。** どの版が動いているかが分からなくなり、
    タスクを入れ替えるたびに中身が変わり得る。
  EOT
  type        = string
  default     = "initial"
}

# ---------------------------------------------------------------------------
# RDS
# ---------------------------------------------------------------------------

variable "db_name" {
  description = "データベース名"
  type        = string
  default     = "tabilog"
}

variable "db_username" {
  description = "データベースの利用者名"
  type        = string
  default     = "tabilog"
}

variable "db_engine_version" {
  description = <<-EOT
    MySQL の版。**メジャー版までしか指定しない。**

    **8.4 系を選ぶ理由**: 8.0 は 2026-04 に標準サポートが終わっており、
    8.4 は LTS で 2032 までサポートされる。
    ap-northeast-1 で 8.4.5〜8.4.11 が利用可能、既定は 8.4.9 であることを
    2026-08-28 に `aws rds` API で確認済み。

    **マイナー版まで固定してはいけない。** auto_minor_version_upgrade を
    有効にしているため、AWS が勝手にマイナー版を上げる。固定していると、
    上がった直後の plan が「8.4.10 を 8.4.9 に戻す」差分を出す。
    設定と実態が食い違い、次に apply する人が意図しない変更を踏む。

    "8.4" と書けば RDS がその時点の既定マイナー版を選び、
    以降の自動更新とも矛盾しない。
  EOT
  type        = string
  default     = "8.4"
}

variable "db_instance_class" {
  description = <<-EOT
    RDS のインスタンスクラス。

    **既定は db.t4g.micro。動作確認のための最小構成である。**
    MySQL 8.4.9 + gp3（最小20GB）で選べることを確認済み（2026-08-30）。

    **想定規模では足りない。** 投稿20万件・いいね200万行に対し、
    micro のバッファプール（実質600MB程度）にはデータと索引が乗り切らない。
    **負荷を測るときは db.t4g.small に戻すこと。**
  EOT
  type        = string
  default     = "db.t4g.micro"
}

variable "db_allocated_storage" {
  description = "ストレージ（GB）。gp3 のこの構成での最小値は 20"
  type        = number
  default     = 20
}

variable "db_max_connections_headroom" {
  description = <<-EOT
    接続数の検算に使う、タスクあたりの接続プール上限。

    **「タスク数 × プール上限 ≦ max_connections」を満たす必要がある。**
    1タスク × 25 = 25。db.t4g.micro（1GB）の max_connections は
    既定の式 {DBInstanceClassMemory/12582880} でおよそ 85 なので成立する。

    **タスクを増やすときは、この式が上限の根拠になる。**
  EOT
  type        = number
  default     = 25
}

variable "imageworker_max_concurrency" {
  description = <<-EOT
    画像処理 Lambda が同時に動きうる数。**接続数の検算に使う。**

    Lambda はそれぞれ RDS への接続を持つため、同時実行の数がそのまま
    接続数に効く。署名付き URL の発行には利用者ごとの上限を設けたが、
    利用者が増えれば全体としては歯止めにならない。

    **予約（reserved_concurrent_executions）では抑えていない。**
    このアカウントの同時実行の上限が 10 で、AWS は「予約していない枠を
    10 未満にできない」ため、1つも予約できないためである。
    上限そのものが歯止めになっている。

        aws lambda get-account-settings --query AccountLimit

    **上限が引き上げられたら、この値を上げたうえで予約を入れ直すこと。**
    1000 になった時点で歯止めが消え、接続の枯渇が現実的になる。
  EOT
  type        = number
  default     = 10
}

variable "imageworker_db_connections" {
  description = <<-EOT
    画像処理 Lambda 1つあたりの接続プール上限。

    画像処理は1回の呼び出しで数回しか問い合わせない。
    API サーバー向けの既定（25）は明らかに過剰で、
    **同時実行のぶんだけ倍になる**ため絞る。
  EOT
  type        = number
  default     = 2
}

variable "db_estimated_max_connections" {
  description = <<-EOT
    インスタンスクラスごとの max_connections の見積もり。

    **控えめに置く。** 実測はもう少し大きいが、余裕を持たせる。
    この値は「超える構成を書いたら plan の時点で落とす」ためだけに使う
    （rds.tf の precondition）。

      db.t4g.micro（1GB） → 約 85
      db.t4g.small（2GB） → 約 170

    **クラスを上げたらこの値も上げること。** 上げ忘れると、
    実際には収まる構成が plan で弾かれる。
  EOT
  type        = number
  default     = 85
}

variable "migrate_image_tag" {
  description = <<-EOT
    マイグレーション用イメージのタグ。バックエンドと同じ ECR リポジトリに置く。

    **バックエンドのイメージとは中身が違う**（migrate バイナリと SQL が入っている）。
    タグで分けているだけなので、`image_tag` と同じ値にしないこと。
  EOT
  type        = string
  default     = "migrate"
}

variable "github_oidc_provider_arn" {
  description = <<-EOT
    既存の GitHub OIDC プロバイダの ARN。空なら新規に作成する。

    **OIDC プロバイダは AWS アカウントに1つしか作れない。**
    同じアカウントで別のリポジトリ（sns-application 等）が既に作っている場合は、
    その ARN をここに渡すこと。渡さないと EntityAlreadyExists で apply が失敗する。
  EOT
  type        = string
  default     = ""
}

variable "github_deploy_subjects" {
  description = <<-EOT
    デプロイ用ロールを引き受けられる GitHub Actions の実行元（OIDC トークンの sub）。

    **ここを絞らないと、GitHub 上のどのリポジトリからでもこのロールを
    引き受けられる。** OIDC 設定で最も多い致命的な誤りがこれ。

    既定は main ブランチからの実行のみ。

    **所有者名とリポジトリ名だけでは一致しない。**
    このリポジトリの sub には ID が埋め込まれている。

        repo:doryu0-04092@292095077/tabi-log@1349507057:ref:refs/heads/main
                        ^^^^^^^^^^          ^^^^^^^^^^
                        所有者 ID           リポジトリ ID

    改名しても別人が乗っ取れないようにするためのもので、
    GitHub 側の既定（`use_default: true`）でこの形になる。

        gh api repos/<所有者>/<名前>/actions/oidc/customization/sub

    **名前だけで書くと `Not authorized to perform sts:AssumeRoleWithWebIdentity`
    で落ちる。** 実際に送られた sub は CloudTrail の
    AssumeRoleWithWebIdentity イベント（userIdentity.userName）で見られる。

    `@ID` を含めずに済ませたい場合はワイルドカードでも書けるが、
    **絞り込みが緩くなるため採らない。** 別のリポジトリが同じ接頭辞を
    持ちうる形にすると、sub を絞る意味が薄れる。
  EOT
  type        = list(string)
  default     = ["repo:doryu0-04092@292095077/tabi-log@1349507057:ref:refs/heads/main"]
}

variable "cd_image_tag_prefix" {
  description = <<-EOT
    CD が積むイメージのタグの接頭辞。

    **ECR のライフサイクルで「消してよいもの」を選ぶために使う。**
    ECR は「このタグは消さない」を書けず、消す対象を選ぶことしかできない。
    素のコミット SHA には共通の接頭辞が無いため対象を絞れず、
    tagStatus = "any" にするしかなくなる。それだと migrate / initial の
    イメージまで巻き込んで消える（どちらも Terraform が名指しで参照する）。

    **`.github/workflows/deploy.yml` のタグの付け方と揃っている必要がある。**
    片方だけ変えると、消えないイメージが溜まり続けるか、
    消してはいけないものが消える。
  EOT
  type        = string
  default     = "sha-"
}

variable "alert_email" {
  description = <<-EOT
    アラームの通知先メールアドレス。**空なら通知しない。**

    空でもアラーム自体は作る。鳴っている事実はコンソールと API から
    見える。**送り先を決めずに通知の仕組みだけ作ると、鳴っていることに
    誰も気づかないまま「監視している」つもりになる。**

    値を入れると購読の確認メールが届く。**承認するまで届かない。**
  EOT
  type        = string
  default     = ""
}
