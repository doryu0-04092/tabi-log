// Package storage は画像の保存先を抽象化する。
//
// インターフェースを1枚挟んでいるのは、配信方式を変えるときの影響範囲を
// このパッケージに閉じるためである。実装は2つある。
//
//   - S3Storage … 署名付き URL。ローカルと LocalStack で使う
//   - CDNSigner … CloudFront + 署名付き Cookie。AWS 上で使う
//
// **この抽象化は実際に効いた。** 署名付き URL から CloudFront へ
// 移したときに変わったのは、このパッケージと Cookie を発行する
// 認証まわりだけで、store と httpapi は手を入れずに済んでいる。
//
// 「インターフェース1枚とアダプタ1個」までに留め、設定駆動の仕組みは作らない。
package storage

import (
	"context"
	"time"
)

// Storage は画像オブジェクトの保存先を表す。
type Storage interface {
	// PresignPut はブラウザから直接アップロードするための URL を返す。
	//
	// contentType と contentLength を署名に含めることで、
	// **条件に合わないアップロードを S3 側で拒否させる**。
	// 申告値そのものは信用せず、アップロード後に中身を検証する。
	PresignPut(ctx context.Context, key, contentType string, contentLength int64, ttl time.Duration) (string, error)

	// DisplayURL は表示用の URL を返す。
	//
	// **署名付き URL とは限らない。** S3 実装は ttl を焼き込んだ
	// 署名付き URL を返すが、CloudFront 実装は固定の URL を返し、
	// 有効期限は Cookie の側が持つ。
	//
	// **名前を PresignGet から変えたのは、「URL 自体が期限切れになる」と
	// 読めてしまうためである。** 固定 URL でなければキャッシュは効かない。
	DisplayURL(ctx context.Context, key string, ttl time.Duration) (string, error)

	// Delete はオブジェクトを消す。存在しない場合もエラーにしない（冪等）。
	//
	// データベースの外部キーは S3 上の実体を消さないため、
	// 投稿の削除時にアプリケーションから明示的に呼ぶ必要がある。
	Delete(ctx context.Context, keys ...string) error
}

// URLSigner は表示用 URL の発行だけを表す。
//
// Storage 全体ではなくこれを要求することで、投稿を読むだけの経路に
// 削除やアップロードの能力を渡さずに済む。
//
// **型を1か所に置いているのは、Go のインターフェースが構造ではなく
// 型で一致を見るためである。** 同じメソッドを持つ別々の型を各層で宣言すると、
// 見た目は同じでも代入できない。
type URLSigner interface {
	DisplayURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}
