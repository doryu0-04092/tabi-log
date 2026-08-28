// Package storage は画像の保存先を抽象化する。
//
// インターフェースを1枚挟んでいるのは、配信方式を変えるときの影響範囲を
// このパッケージに閉じるためである。現在は S3 の署名付き URL で配信するが、
// AWS へ載せる際は CloudFront の署名付き Cookie に移す予定であり、
// そのとき上位層（store / httpapi）は変更せずに済む。
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

	// PresignGet は表示用の URL を返す。
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)

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
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}
