# インフラ（Terraform）

`docs/aws-architecture.md` の構成を Terraform で書いたもの。

> **状態: `plan` まで通した。`apply` はしていない。**（2026-08-29）
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

docker build -f ../docker/Dockerfile.backend -t "$ECR:initial" ..
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

## 本番運用するなら変えるところ

**以下は意図的に費用を優先した箇所である。**

| 箇所 | 現在 | 本番では |
|---|---|---|
| Fargate の配置 | パブリックサブネット + パブリック IP | プライベート + NAT、または VPC エンドポイント群 |
| RDS | Single-AZ・削除保護なし・保持1日 | Multi-AZ・削除保護あり・保持7〜35日 |
| タスク数 | 2（固定） | Auto Scaling。**判断の指標は ECS の CPU ではなく RDS の CPU** |
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
