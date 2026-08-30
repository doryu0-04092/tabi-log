# インフラ（Terraform）

`docs/aws-architecture.md` の構成を Terraform で書いたもの。

> **状態: 2026-08-30 に `apply` し、検証後 `destroy` 済み。** 現在リソースは無い。
> 手順と結果は `docs/aws-architecture.md`、
> 未検証の項目は `docs/deploy-verification.md` を参照。
>
> 直近の plan の結果（apply 前）:
>
> ```
> Plan: 74 to add, 0 to change, 0 to destroy.
> ```
>
> エラーも警告も出ていない。**AWS に問い合わせるデータソースがすべて解決した**
> ことまでは確かめられた。
>
> | 確かめられたこと | 結果 |
> |---|---|
> | CloudFront のマネージドプレフィックスリスト | `pl-58a04531` に解決 |
> | `/api/*` のキャッシュポリシー | CachingDisabled に解決 |
> | `/variants/*` のキャッシュポリシー | CachingOptimizedForUncompressedObjects に解決 |
> | 引数の組み合わせ | プロバイダの検証を通過 |
> | 接続数の見積もり（precondition） | 通過 |
>
> **`apply` はしていないので、動いた記録ではない。** 実際に作れるか、
> 作ったものが期待どおり動くかは別である。

## 何が作られるか

```
CloudFront（唯一の窓口）
├── default  → S3（静的サイト・OAC で非公開のまま）
└── /api/*   → ALB → ECS Fargate（backend:8080）→ RDS MySQL 8.4
                                                    ↑
S3（画像・完全非公開）── PutObject ──→ Lambda（Go・EXIF 除去と変換）
```

| ファイル | 内容 |
|---|---|
| `network.tf` | VPC・サブネット・ルート・**S3 のゲートウェイエンドポイント**・セキュリティグループ4つ |
| `alb.tf` | ALB・ターゲットグループ・**既定 403 のリスナー** |
| `ecs.tf` | ECR・クラスタ・タスク定義・サービス |
| `rds.tf` | サブネットグループ・**ngram のパラメータグループ**・インスタンス |
| `s3.tf` | 画像バケット・静的サイトバケット・ライフサイクル・イベント通知 |
| `lambda.tf` | 画像処理 Lambda |
| `cloudfront.tf` | ディストリビューション |
| `iam.tf` | 実行ロール・タスクロール・Lambda ロール |
| `secrets.tf` | パスワードの生成と Parameter Store への登録 |

## 使い方

### 準備

```bash
cp terraform.tfvars.example terraform.tfvars
# バケット名を自分の値に書き換える（全 AWS アカウントで一意である必要がある）

# 画像処理 Lambda の成果物を先に作る。無いと plan の時点で落ちる。
bash ../docker/build-imageworker.sh

terraform init
```

### 確認と適用

```bash
terraform plan     # 何が作られるかを見る
terraform apply
```

**`apply` だけでは動かない。** 中身を入れる作業が3つ残る。

### 1. バックエンドのイメージを push する

```bash
ECR=$(terraform output -raw ecr_repository_url)
aws ecr get-login-password | docker login --username AWS --password-stdin "${ECR%%/*}"

# **--provenance=false --sbom=false を外さないこと。**
# 付けないと buildx が attestation を含む OCI イメージインデックスを作り、
# ECS Fargate がイメージを取得できないことがある。
# sns-application が実際にこれで詰まっている。
#
# --platform も要る。Fargate は linux/amd64 で、開発機が別アーキテクチャだと
# そのままでは起動しない。
docker build --platform linux/amd64 --provenance=false --sbom=false \
  -f ../docker/Dockerfile.backend -t "$ECR:initial" ..
docker push "$ECR:initial"

# タスクを入れ替える
aws ecs update-service --force-new-deployment \
  --cluster "$(terraform output -raw ecs_cluster)" \
  --service "$(terraform output -raw ecs_service)"
```

### 2. マイグレーションを適用する

**データベースはプライベートサブネットにあり、手元からは届かない。**
`terraform output db_endpoint` の値に直接つなぐことはできない。

方法は2つある。

- **ECS のタスクから流す**（`aws ecs run-task` で migrate のイメージを1回動かす）
- 一時的に踏み台を立てて、そこから流す

**起動時の自動適用はしない。** スキーマの変更をデプロイから切り離すためである
（この方針はローカルの docker compose でも同じにしてある）。

### 3. フロントエンドを配置する

```bash
cd ../frontend && npm run build && cd ../infra

aws s3 sync ../frontend/build "s3://$(terraform output -raw static_bucket)" --delete

# **index.html だけは無効化する。** ファイル名にハッシュが付かないため、
# 消さないと古いものが配られ続ける。
aws cloudfront create-invalidation \
  --distribution-id "$(terraform output -raw cloudfront_distribution_id)" \
  --paths "/index.html"
```

## デプロイの自動化（CD）

`.github/workflows/deploy.yml` が、上の1〜3を自動で行う。
**`terraform apply` は含まない。** state をローカルで管理しており（`versions.tf`）、
CI から共有できないためである。インフラも CD に載せるなら、先に state を S3 へ移す。

### 認証

**アクセスキーは発行しない。** GitHub の OIDC トークンを AWS に信頼させ、
実行のたびに一時的な認証情報を受け取る（`infra/cicd.tf`）。

信頼ポリシーでは `aud` と `sub` の両方を確認する。
**`sub` を絞らないと、GitHub 上のどのリポジトリからでもこのロールを引き受けられる。**
許可する実行元は `github_deploy_subjects` 変数で、既定は main ブランチのみ。

> **OIDC プロバイダは AWS アカウントに1つしか作れない。**
> このアカウントでは sns-application も同じものを作る。**両方を同時にデプロイする場合は、
> 後から作る側が `github_oidc_provider_arn` に既存の ARN を受け取る**こと。
> 渡さないと `EntityAlreadyExists` で apply が失敗する。

### 初回の設定

`terraform apply` 後、出力値をリポジトリの **Variables** に登録する
（Secrets でなくてよい。ARN やバケット名は秘密ではなく、引き受けられるのは
信頼ポリシーで許可した実行元だけのため）。

```bash
cd infra
gh variable set AWS_DEPLOY_ROLE_ARN            --body "$(terraform output -raw github_actions_role_arn)"
gh variable set AWS_STATIC_BUCKET              --body "$(terraform output -raw static_bucket)"
gh variable set AWS_CLOUDFRONT_DISTRIBUTION_ID --body "$(terraform output -raw cloudfront_distribution_id)"
gh variable set AWS_APP_URL                    --body "$(terraform output -raw app_url)"  # スキーム込みのURLが返る
```

**CloudFront のドメインと ID は、作り直すたびに変わる。**
destroy 後も古い値が残るため、次に apply したら必ず登録し直すこと。
古い値が残っていても、CD は最初の「ECS サービスが存在するか」の確認で止まるため
誤った環境へデプロイすることはない。**ただし値そのものは信用してはいけない。**

### 実行

**手動起動（`workflow_dispatch`）にしている。**
このプロジェクトは「使わない期間は destroy する」運用のため、
main への push で自動デプロイすると環境が無い期間はマージのたびに失敗し、
**赤いバッジが常態化して誰も見なくなる**。
常時稼働に変えるなら `on:` に `push` を足すだけでよい。

```bash
gh workflow run Deploy
gh workflow run Deploy -f run_migrations=true          # スキーマを変えたとき
gh workflow run Deploy -f deploy_frontend=false        # バックエンドだけ
```

### マイグレーションの順序（**ここが sns-application と最も違う**）

**tabi-log は起動時に自動適用しない。** スキーマの変更をデプロイから切り離すためである。
そのぶん、変えたときは `run_migrations=true` を指定する必要がある。

CD は**マイグレーションをアプリより先に流す**。逆にすると、
新しいコードが古いスキーマの上で動く時間ができるためである。

**ただし、列を削除する変更ではこの順序が逆に働く。**
先に消すと、まだ動いている古いコードが落ちる。その場合は分けること。

| 変更の種類 | 進め方 |
|---|---|
| 列やテーブルを**足す** | そのまま `run_migrations=true` で1回 |
| 列を**消す** | ①使わないコードを配る → ②消す移行を流す、の**2回に分ける** |
| 列の**型を変える** | 足す→両方書く→移す→古い方を消す。**自動化できない** |

**CD が自動化できるのは前半だけである。** ここを一括で流せる形にすると、
「消す移行」を含む変更で本番が落ちる。

### 何を確かめてから成功とするか

| 段階 | 確認 |
|---|---|
| 開始前 | ECS サービスが ACTIVE か。**destroy 済みならここで止める** |
| マイグレーション | **タスクの終了コード**。停止しただけでは成功とは限らない。見落とすとスキーマが古いままアプリだけ新しくなる。ログも必ず出す |
| バックエンド | `ecs wait services-stable`。**待たずに終えると、起動に失敗して古いタスクへ戻っていても「成功」と表示される** |
| フロントエンド | `cloudfront wait invalidation-completed` |
| 最後 | **CloudFront 経由で `/api/readyz` が 200**。ECS が安定しただけでは経路が通っている保証にならない。`readyz` は DB 疎通も見るため、移行の失敗もここで表に出る |

### CD に含めていないもの

| 対象 | 理由 |
|---|---|
| `terraform apply` | state がローカルにあり CI から共有できない |
| **画像処理 Lambda** | Terraform が `source_code_hash` で管理している。CD から更新すると次の apply で巻き戻る。**変更したら `terraform apply` で配ること** |

### タスク定義の所有者

CD は**現行のタスク定義のイメージだけを差し替えて**新しいリビジョンを登録する。
環境変数・秘密・CPU/メモリには触れない。作り直すと Terraform の定義と食い違うためである。

あわせて `aws_ecs_service.backend` に `ignore_changes = [task_definition]` を付けた。
**これが無いと、デプロイ後の `terraform apply` がサービスを Terraform 側の
リビジョンへ黙って戻す。** 障害時にロールバックしていた場合、
それを勝手に取り消して再び壊れた版を配ることになる。

| 対象 | 所有者 |
|---|---|
| サービスの構成（ネットワーク・LB・サーキットブレーカー） | Terraform |
| **どのリビジョンが動いているか** | CD |

### 未検証の点（正直に）

**このワークフローはまだ一度も実行していない。** 現在 AWS 上にリソースが無いためである。
確認できているのはここまで。

- ワークフロー YAML の構文（パーサで検証）
- 各 `run` ブロックのシェル構文（`bash -n`、11ブロックすべて）
- Terraform の構文と整合（`terraform validate`）
- `app_url` がスキーム込みで返ること（sns-application で二重スキームの不具合を踏んだため確認した）

**確認できていないのは、実際に AWS に対して通るかどうかである。**
IAM の権限が過不足なく足りているかは、一度デプロイするまで分からない。

---

### 片付け

```bash
terraform destroy
```

**固定費の大半は ALB・Fargate・RDS で、起動しているだけで課金される**
（月$90〜100 程度）。学習目的で常時動かす必要はないため、
使わない期間は消す前提の設計にしてある
（削除保護を外し、最終スナップショットも取らない）。
**構成はコードとして残るので、必要になったら作り直せる。**

## 設計上の判断（コードを読む前に知っておくとよいこと）

### ALB を守っているのはヘッダーであってセキュリティグループではない

CloudFront の既定ドメインだけで完結させる構成では、内部 ALB も VPC オリジンも
使えないため、ALB はインターネット向けにせざるを得ない。二重に絞っている。

1. セキュリティグループを CloudFront のマネージドプレフィックスリストに限定
2. **CloudFront が付ける合言葉のヘッダーを条件にし、既定を 403 にする**

**1 は「CloudFront から来たこと」しか保証しない。** 他人が作った
ディストリビューションも同じ IP レンジから来る。**実際の防御線は 2 である。**

### ヘルスチェックはデータベースを見ない

ALB のターゲットグループには `/api/livez` を向けている。

疎通まで見るヘルスチェックをロードバランサに向けると、データベースが
一時的に不調になったときに**全タスクが同時に unhealthy と判定されて
一斉に置き換えられる**。置き換えてもデータベースは回復しない。
タスクを入れ替えるべき理由になるのは「プロセスが生きていないこと」だけである。

### Lambda が S3 に届くためにゲートウェイエンドポイントが要る

画像処理 Lambda はデータベースを更新するため VPC の中に置く。
プライベートサブネットには NAT が無いので、**そのままでは S3 に到達できず、
画像を読めないまま毎回タイムアウトする。**

ゲートウェイ型のエンドポイントは無料で、ルートテーブルに経路を足すだけである。
（設計書の構成図には Lambda → S3 の線があるが、この経路の手当ては
書かれていなかった。Terraform を書く過程で気づいた。）

### Lambda の秘密だけ Parameter Store を経由しない

同じ理由で、プライベートサブネットから SSM へ届くには**インターフェース型の
エンドポイント（時間課金）**が要る。画像処理のためだけに常時課金を足す判断は
取らず、Lambda の環境変数に直接入れている。

代償として、値が Lambda の設定に入り、権限のある人はコンソールから読める。
**Terraform の state に平文で入るのと同じ性質の割り切り**である。

### 画像は CloudFront から配り、読む権利は Cookie が持つ

**以前は S3 の署名付き URL を返していた。** 署名付き URL は呼ぶたびに
署名と時刻が変わるため、**同じ画像でも毎回別の URL** になる。その結果、
エッジのキャッシュもブラウザのキャッシュも一度も当たっていなかった。
画像が主役のサービスで、転送量の9割以上を占める経路である。

URL を `/variants/<鍵>` に固定し、読む権利を**署名付き Cookie**へ移した。
URL が変わらないので、エッジにもブラウザにも載る。

**署名を URL ではなく Cookie に載せたのは枚数の都合である。**
フィード1ページで最大80枚を返すため、URL ごとに署名すると応答が
署名文字列で埋まる（1本500文字前後）。Cookie なら1セットで全部に効く。

**配信するのは `variants/` 配下だけである。** `originals/` には
アップロードされたままの画像があり、**EXIF（GPS 座標）が残っている**。
ビヘイビアとバケットポリシーの二重で絞っている。片方だけだと、
もう片方を緩めたときに気づけない。

**アップロードは CloudFront を通らない。** ブラウザは S3 の署名付き URL へ
直接 PUT する。CloudFront は読む側だけを担う。

## 既定は「動作確認のための最小構成」である

**想定規模を捌く構成ではない。** 常時動かさない前提で、確認して destroy する
温度感に合わせてある（課題の資料でも「作って動作確認したら即消すくらいの
温度感でよい」とされている）。

| 項目 | 既定（確認用） | 想定規模で測るとき |
|---|---|---|
| Fargate | 0.25 vCPU / 512MB × **1タスク** | 0.5 vCPU / 1GB × 2タスク |
| RDS | **db.t4g.micro** | db.t4g.small |
| `db_estimated_max_connections` | 85（micro の目安） | 170（small の目安） |

**1タスクではデプロイ中と障害時に止まる。** 冗長性が要るなら 2 にする。

**この構成で測った数字は、想定構成の性能を表さない。** 負荷を測るときは
上の右列に戻すこと（`terraform.tfvars` で上書きできる）。

```hcl
# 想定規模で測るとき
task_cpu                     = 512
task_memory                  = 1024
desired_count                = 2
db_instance_class            = "db.t4g.small"
db_estimated_max_connections = 170
```

**クラスを上げたら `db_estimated_max_connections` も上げること。**
上げ忘れると、実際には収まる構成が plan の precondition で弾かれる。

## 本番運用するなら変えるところ

**以下は意図的に費用を優先した箇所である。**

| 箇所 | 現在 | 本番では |
|---|---|---|
| Fargate の配置 | パブリックサブネット + パブリック IP | プライベート + NAT、または VPC エンドポイント群 |
| RDS | Single-AZ・削除保護なし・保持1日 | Multi-AZ・削除保護あり・保持7〜35日 |
| タスク数 | 1（固定） | Auto Scaling。**判断の指標は ECS の CPU ではなく RDS の CPU** |
| ドメイン | CloudFront の既定 | 独自ドメイン + ACM(us-east-1) + Route53 |
| WAF | なし | CloudFront に付ける |
| レート制限 | アプリのメモリ内 | **2タスクでは実効的な上限が2倍になる。** ElastiCache か WAF |
| state | ローカル | S3 バックエンド + ロック |
| ECR のタグ | MUTABLE | IMMUTABLE（動いている版が変わらないように） |
| デプロイ | 手動 | GitHub Actions から |

## まだ確認していないこと

- **`apply` していない。** `plan` が通ることは「作れる見込みがある」
  ことであって、作れることでも、動くことでもない
- **画像の CloudFront 配信は、ローカルでも E2E でも通っていない。**
  LocalStack に CloudFront が無いためである。
  仕様から取れる条件（署名の形式・ポリシーの制約・Cookie の属性）は
  `internal/storage/cloudfront_test.go` に落としてあり、
  **AWS が公開している符号化の例と一致することまでは確認済み**だが、
  実際に CloudFront が受け付けるかは apply しないと分からない
