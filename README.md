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

> **画像を扱う場合は、起動前に画像処理 Lambda をビルドしてください。**
> LocalStack の初期化スクリプトがこの成果物を読み込んで Lambda を作り、
> S3 のイベント通知を設定します。無い場合は S3 だけが作られ、
> アップロードした画像が `processed` にならず投稿できません。
>
> ```bash
> bash docker/build-imageworker.sh
> docker compose up -d   # 既に起動している場合は down してから
> ```

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

> **シェルスクリプトを追加したときの注意（Windows）**
> Git for Windows は既定で `core.fileMode=false` のため、`chmod +x` が
> コミットに反映されません。実行権限が要るスクリプトは明示的に記録します。
>
> ```bash
> git update-index --chmod=+x path/to/script.sh
> ```
>
> これを忘れると、手元では動くのに CI で `Permission denied` になります
> （LocalStack の初期化スクリプトで実際に踏みました）。

### 画像の扱い

画像は**ブラウザから S3 へ直接送ります**（サーバーを経由しません）。
大きなファイルでバックエンドの帯域とタイムアウトがボトルネックになるのを避けるためです。

```
1. POST /api/media/presign  → media を pending で記録し、署名付きURLを返す
2. ブラウザ → S3           → 署名付きURLへ直接 PUT
3. S3 イベント → Lambda    → 形式検証・EXIF除去・変換物の生成 → processed へ
4. POST /api/posts         → processed の画像だけを投稿に紐づけられる
```

**バックエンドは画像の中身を一度も見ません。** そのため中身を検証できる唯一の場所が
Lambda（`backend/cmd/imageworker`）になります。ここで行うことは3つです。

- 拡張子や申告された Content-Type ではなく、**バイト列から実際の形式を判定する**
- **EXIF（GPS座標・撮影日時・端末情報）を除去する** — 位置情報を都道府県までに
  絞っていても、画像に GPS が残っていれば意味がなくなります
- EXIF を消すと**向きの情報も消える**ため、消す前に読み取って画素を回転させます
  （これをしないとスマートフォンの縦写真が横倒しになります）

変換の中身は `backend/internal/media` にあり、S3 にもデータベースにも依存しません。
何も起動せずにテストできます。

### コード生成

このプロジェクトには生成物が2系統あります。**どちらも「自分で書いたものが正」で、
そこからコードを作ります。逆向きではありません。**

| 入力（正） | 生成物 | ツール |
|---|---|---|
| `docs/openapi.yaml` | `backend/internal/api/gen/`（Go の型と ServerInterface）<br>`frontend/src/lib/api/gen.ts`（TypeScript の型） | oapi-codegen / openapi-typescript |
| `backend/db/migrations/` ＋ `backend/db/queries/` | `backend/internal/store/dbgen/`（型安全な DB アクセス関数） | sqlc |

```bash
cd backend  && go generate ./...   # oapi-codegen と sqlc の両方
cd frontend && npm run gen:api
```

**生成物は直接編集しません。** 入力を変えて再生成し、コミットしてください。
CI が再生成して**未追跡ファイルを含めて**差分の有無を検証します。

新しいクエリを追加するときは `backend/db/queries/*.sql` に SQL を書き、
`-- name: 関数名 :one|:many|:exec` の注釈を付けて再生成します。

> **sqlc が扱えないもの**: 条件が実行時に増減する検索クエリ（都道府県で絞る／絞らない、
> タグで絞る／絞らない の組み合わせ）は、SQL 文自体が変化するため生成できません。
> 検索リポジトリだけは `database/sql` で手書きします。その際、値は必ず
> プレースホルダにバインドし、ユーザー入力を SQL 文字列に連結しません。

### テスト

```bash
# バックエンド
cd backend
go test ./...
golangci-lint run   # CI と同じ検査。未導入なら:
                    #   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

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
- `GET /api/prefectures` — 47件を JIS コード順で返す
- 生成の仕組み2系統（OpenAPI からの型生成 / sqlc による DB アクセス生成）
- CI（型検査・lint・単体・E2E・アクセシビリティ・脆弱性検査・生成物の一致検証）

機能の実装（認証・投稿・反応・フィード・発見・通知）と AWS へのデプロイはこれからです。

## ライセンス

MIT License（[LICENSE](LICENSE)）
