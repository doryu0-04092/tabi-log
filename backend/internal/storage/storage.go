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
// オブジェクトの状態を表すタグ。**ライフサイクルの判断材料になる。**
//
// S3 のライフサイクルは「タグが無いこと」を条件にできない。そのため
// **確定していない側にタグを付ける**形にしてある。付ける役はクライアントで、
// presign の署名にタグを焼き込んで S3 に強制させる。
//
// **Lambda に付けさせてはいけない。** Lambda が失敗した原本にタグが付かず、
// 永久に消えなくなる。掃除したいのはまさにその孤児である。
const (
	StateTagKey = "state"

	// StateTagPending は「まだ投稿に使われていない」ことを表す。
	// ライフサイクルはこのタグが付いたものだけを期限で消す。
	StateTagPending = "pending"

	// StateTagKept は「投稿に使われた」ことを表す。**消さない。**
	// 原本を残すのは、別解像度を後から作れるようにするためである
	// (docs/er-diagram.md)。変換物からの再エンコードは非可逆で、
	// 一度失うと取り戻せない。
	StateTagKept = "kept"
)

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

	// MarkKept は「投稿に使われた」印を付け、期限削除の対象から外す。
	//
	// **投稿が確定した時点で呼ぶ。** 呼ばれなかったオブジェクトは
	// pending のまま残り、ライフサイクルが期限で消す。
	//
	// 存在しないキーを渡してもエラーにしない（冪等）。
	MarkKept(ctx context.Context, keys ...string) error

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
