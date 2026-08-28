# tabi-log

旅行先の写真と記録を投稿し、他の利用者と共有する SNS アプリケーションです。
投稿に**都道府県**と**訪問日**を必ず紐づけることで、単なる写真投稿ではなく
「どこへ、いつ行ったか」が構造化されて残る点が、汎用の SNS との違いです。

スクール課題として作成しています。学習目的の構成であり、商用サービスではありません。

## ドキュメント

| 文書 | 内容 |
|---|---|
| [要件定義書](docs/requirements.md) | 何を作るか、何を作らないか、非機能要件 |
| [機能一覧](docs/features.md) | 機能の一覧と受け入れ条件 |
| [技術スタック](docs/tech-stack.md) | 技術選定とその理由、前回プロジェクトとの差分 |
| [ER図](docs/er-diagram.md) | テーブル定義とインデックス設計 |
| [API仕様](docs/openapi.yaml) | OpenAPI 仕様。**これが唯一の正**であり、コードはここから生成します |
| [AWS構成設計](docs/aws-architecture.md) | インフラ構成と設計判断 |
| [画面設計](docs/screens.md) | 画面一覧と遷移 |
| [テスト計画](docs/test-plan.md) | テストの範囲と方針 |
| [運用設計](docs/operations.md) | ログ・監視・障害対応 |

## 構成

```
[SvelteKit (SPA)]  ──  REST/JSON  ──  [Go (net/http)]  ──  [MySQL 8.4]
   CloudFront + S3                      ALB + ECS Fargate      RDS
                                            │
                                     [S3] ──┴── [Lambda (Go)]
                                    画像      EXIF除去・変換
```

## ローカル開発環境

### 必要なもの

| ツール | バージョン |
|---|---|
| Go | 1.27 |
| Node.js | 24（Active LTS） |
| Docker Desktop | 任意の最新版 |
| Terraform | 1.9 以降（インフラを触る場合のみ） |

`make` は使いません（Windows 環境で追加導入が要るため）。
繰り返し使うコマンドは下記にそのまま並べています。

### 起動手順

```bash
# 1. 環境変数を用意する
cp .env.example .env

# 2. MySQL・LocalStack・バックエンドを起動する
docker compose up -d --build

# 3. マイグレーションを適用する
#    起動時の自動適用はしません。スキーマ変更をデプロイから切り離すためです。
docker compose run --rm migrate up

# 4. フロントエンドを起動する（ホットリロードを優先しネイティブ起動）
cd frontend && npm install && npm run dev
```

<http://localhost:5173> を開くと、バックエンドとの疎通結果が表示されます。

| サービス | ポート |
|---|---|
| フロントエンド | 5173 |
| バックエンド | 8080 |
| MySQL | 3306 |
| LocalStack (S3) | 4566 |

バックエンドを頻繁に変更する場合は、依存だけコンテナで起動してネイティブ実行する方が反復が速くなります。

```bash
docker compose up -d mysql localstack
cd backend && go run ./cmd/server
```

### マイグレーション

```bash
docker compose run --rm migrate up          # 適用
docker compose run --rm migrate down 1      # 1つ戻す
docker compose run --rm migrate version     # 現在の版
```

新しいマイグレーションは `backend/db/migrations/` に
`000003_<内容>.up.sql` と `.down.sql` の対で追加します。**適用済みのファイルは変更しません。**

### コード生成（spec-first）

API の型は `docs/openapi.yaml` から生成します。**逆向きではありません。**
仕様を変更したら再生成してコミットしてください。CI が差分の有無を検証します。

```bash
cd backend  && go generate ./...   # -> internal/api/gen/api.gen.go
cd frontend && npm run gen:api     # -> src/lib/api/gen.ts
```

### テスト

```bash
# バックエンド
cd backend && go test ./...

# フロントエンド
cd frontend
npm run check     # 型検査 + Svelte の a11y 警告（警告もエラー扱い）
npm run lint
npm test          # 単体（Vitest）
npm run test:e2e  # E2E + axe-core によるアクセシビリティ検査（Playwright）
```

### ヘルスチェック

```bash
curl http://localhost:8080/api/livez   # プロセスの生存のみ。DBを見ない
curl http://localhost:8080/api/readyz  # DB疎通を含む
```

**この2つを分けているのは意図的です。** 単一のヘルスチェックが DB 疎通まで見ると、
DB が一時的に不調になったときに全タスクが unhealthy と判定されて一斉に置き換えられます。
置き換えても DB は回復しないため状況が悪化するだけです。
ロードバランサのヘルスチェックには `/api/livez` を向けます。

動作を確認するには、MySQL を止めて両者の応答を比べてください。

```bash
docker compose stop mysql
curl -i http://localhost:8080/api/livez    # 200 のまま
curl -i http://localhost:8080/api/readyz   # 503
docker compose start mysql
```

## 現在の状況

立ち上げ段階です。以下が動作します。

- ヘルスチェック（`/api/livez` / `/api/readyz`）と、その分離の検証
- 全テーブルのスキーマと都道府県マスタ47件
- 日本語の全文検索（MySQL の ngram パーサ）
- OpenAPI 仕様からの Go / TypeScript 型生成
- CI（型検査・lint・単体・E2E・アクセシビリティ・脆弱性検査・生成物の一致検証）

機能の実装（認証・投稿・反応・フィード・発見・通知）と AWS へのデプロイはこれからです。

## ライセンス

MIT License（[LICENSE](LICENSE)）
