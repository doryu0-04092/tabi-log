package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"
)

/*
消すべき S3 のオブジェクトを控える。

投稿の削除・退会では、データベースを先に確定させてから S3 を消す。
「S3 は消えたがデータベースに残る」より「データベースは消えたが
S3 に残る」ほうが害が小さいためである。

**だが、後者を拾う仕組みが無かった。** 行が消えた時点で鍵を辿れなくなり、
S3 の削除が失敗するとオブジェクトが永久に残る。原本には `state=kept` が
付いておりライフサイクルの対象外で、変換物にはライフサイクル自体が無い
（消すと表示中の投稿が壊れるため置けない）。

`docs/audit-2026-08-31.md` M5。
*/

// ObjectDeletionQueue は消し損ねたオブジェクトの控え。
type ObjectDeletionQueue struct {
	q *dbgen.Queries
}

func NewObjectDeletionQueue(db *sql.DB) *ObjectDeletionQueue {
	return &ObjectDeletionQueue{q: dbgen.New(db)}
}

// Enqueue は鍵を控える。
//
// **1件失敗しても残りを続ける。** ここで止めると、控えられた鍵と
// 控えられなかった鍵が混ざり、どこまで進んだか分からなくなる。
func (s *ObjectDeletionQueue) Enqueue(ctx context.Context, keys ...string) error {
	var firstErr error
	for _, k := range keys {
		if err := s.q.EnqueueObjectDeletion(ctx, k); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("削除待ちに控えられない (%s): %w", k, err)
		}
	}
	return firstErr
}
