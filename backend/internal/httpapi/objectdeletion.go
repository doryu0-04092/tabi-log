package httpapi

import (
	"context"
	"log/slog"
	"net/http"
)

/*
S3 のオブジェクトを消し、消せなかったものを控える。

投稿の削除・退会では、データベースを先に確定させてから S3 を消す。
「S3 は消えたがデータベースに残る」より「データベースは消えたが
S3 に残る」ほうが害が小さいためである。

**だが、控えないと後者を拾えない。** 行が消えた時点で鍵を辿れなくなり、
オブジェクトが永久に残る。原本には `state=kept` が付いていて
ライフサイクルの対象外、変換物にはライフサイクル自体が無い
（消すと表示中の投稿が壊れるため置けない）。

`docs/audit-2026-08-31.md` M5。
*/

// DeletionQueue は消し損ねたオブジェクトの控え先。
type DeletionQueue interface {
	Enqueue(ctx context.Context, keys ...string) error
}

// deleteObjects は S3 から消し、消せなければ控えて掃除に任せる。
//
// **利用者への応答は変えない。** データベース上は既に消えており、
// 利用者から見て消えている。ここで失敗を返すと「消せていない」と
// 誤解させる。
func deleteObjects(r *http.Request, objects ObjectStorage, queue DeletionQueue, logger *slog.Logger, what string, keys []string) {
	if len(keys) == 0 {
		return
	}
	if err := objects.Delete(r.Context(), keys...); err == nil {
		return
	} else {
		logger.ErrorContext(r.Context(), what,
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Int("keys", len(keys)),
			slog.String("error", err.Error()),
		)
	}

	if queue == nil {
		return
	}
	if err := queue.Enqueue(r.Context(), keys...); err != nil {
		// **ここまで失敗すると鍵は失われる。** S3 にもデータベースにも
		// 残らないため、記録が唯一の手がかりになる。鍵そのものを残す。
		logger.ErrorContext(r.Context(), "消せなかったオブジェクトを控えられなかった。鍵はここにしか残らない",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Any("keys", keys),
			slog.String("error", err.Error()),
		)
	}
}
