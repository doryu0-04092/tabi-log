// Package janitor は溜まり続けるものを定期的に片付ける。
//
// **どれも放っておいても例外は出ない。** 行が増え、S3 のオブジェクトが
// 増え、費用と検索の重さだけが静かに悪化する。動かして気づくことはない。
//
// 専用のバッチや Lambda を立てず、アプリケーションの中で回している。
// **タスクが複数あると同じ掃除が重なって走るが、削除は冪等なので
// 害が無い。** 排他のために外部の仕組み（分散ロック）を足すほうが、
// この規模では複雑さに見合わない。
//
// `docs/audit-2026-08-31.md` M4 / M6。
package janitor

import (
	"context"
	"log/slog"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

// Store は掃除に必要なデータベース操作。
type Store interface {
	DeleteExpiredRefreshTokens(ctx context.Context, before time.Time, limit int32) (int64, error)
	ListOrphanMedia(ctx context.Context, before time.Time, limit int32) ([]store.OrphanMedia, error)
	DeleteMedia(ctx context.Context, mediaID uint64) error

	// 消し損ねた S3 のオブジェクトの控え。
	// **行が消えたあとの削除失敗は、ここにしか残らない。**
	ListPendingDeletions(ctx context.Context, limit int32) ([]store.PendingDeletion, error)
	ForgetPendingDeletion(ctx context.Context, id uint64) error
	RecordDeletionAttempt(ctx context.Context, id uint64) error
}

// ObjectDeleter は S3 のオブジェクトを消す。
type ObjectDeleter interface {
	Delete(ctx context.Context, keys ...string) error
}

// Config は掃除の間隔と、どこまで遡って消すか。
type Config struct {
	// Interval は掃除を回す間隔。
	Interval time.Duration

	// MediaRetention は「参照されていない画像」を消すまでの猶予。
	//
	// **S3 のライフサイクル（既定7日）より短くしない。** 短いと、
	// アップロード中の画像の行を先に消してしまい、Lambda が
	// 「知らない画像だ」と失敗する。
	MediaRetention time.Duration

	// TokenRetention は期限切れトークンの行を残す猶予。
	// 期限が過ぎたトークンは提示されても通らないため、短くてよい。
	TokenRetention time.Duration

	// BatchLimit は1回の掃除で扱う最大件数。
	// **区切らないと、溜まっていた場合に長時間の処理になる。**
	BatchLimit int32
}

// DefaultConfig は既定値。
func DefaultConfig() Config {
	return Config{
		Interval:       time.Hour,
		MediaRetention: 8 * 24 * time.Hour,
		TokenRetention: 24 * time.Hour,
		BatchLimit:     500,
	}
}

type Janitor struct {
	store   Store
	objects ObjectDeleter
	cfg     Config
	logger  *slog.Logger
	now     func() time.Time
}

func New(s Store, objects ObjectDeleter, cfg Config, logger *slog.Logger) *Janitor {
	return &Janitor{store: s, objects: objects, cfg: cfg, logger: logger, now: time.Now}
}

// Run は ctx が終わるまで一定間隔で掃除する。
//
// **起動直後には回さない。** デプロイのたびに全タスクが同時に
// 掃除を始めることになり、切り替えで最も負荷が高い時間帯と重なる。
func (j *Janitor) Run(ctx context.Context) {
	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.RunOnce(ctx)
		}
	}
}

// RunOnce は1回分の掃除を行う。
//
// **途中で失敗しても次の回で拾えるため、エラーで止めない。**
// 記録だけ残して次の対象に進む。
func (j *Janitor) RunOnce(ctx context.Context) {
	now := j.now()

	n, err := j.store.DeleteExpiredRefreshTokens(ctx, now.Add(-j.cfg.TokenRetention), j.cfg.BatchLimit)
	if err != nil {
		j.logger.ErrorContext(ctx, "期限切れトークンの掃除に失敗した", "error", err)
	} else if n > 0 {
		j.logger.InfoContext(ctx, "期限切れトークンを掃除した", "deleted", n)
	}

	j.cleanOrphanMedia(ctx, now)
	j.drainPendingDeletions(ctx)
}

// drainPendingDeletions は消し損ねた S3 のオブジェクトを消し直す。
//
// **控えを消すのは、S3 から消せたときだけ。** 消せていないのに
// 控えを消すと、そのオブジェクトを辿る手段が永久に無くなる。
func (j *Janitor) drainPendingDeletions(ctx context.Context) {
	pending, err := j.store.ListPendingDeletions(ctx, j.cfg.BatchLimit)
	if err != nil {
		j.logger.ErrorContext(ctx, "削除待ちのオブジェクトを取得できなかった", "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	deleted := 0
	for _, p := range pending {
		if err := j.objects.Delete(ctx, p.S3Key); err != nil {
			j.logger.ErrorContext(ctx, "削除待ちのオブジェクトを消せなかった",
				"key", p.S3Key, "error", err)
			if err := j.store.RecordDeletionAttempt(ctx, p.ID); err != nil {
				j.logger.ErrorContext(ctx, "削除の試行回数を記録できなかった",
					"id", p.ID, "error", err)
			}
			continue
		}
		if err := j.store.ForgetPendingDeletion(ctx, p.ID); err != nil {
			j.logger.ErrorContext(ctx, "削除待ちの控えを消せなかった",
				"id", p.ID, "error", err)
			continue
		}
		deleted++
	}
	j.logger.InfoContext(ctx, "消し損ねていたオブジェクトを掃除した",
		"deleted", deleted, "found", len(pending))
}

func (j *Janitor) cleanOrphanMedia(ctx context.Context, now time.Time) {
	orphans, err := j.store.ListOrphanMedia(ctx, now.Add(-j.cfg.MediaRetention), j.cfg.BatchLimit)
	if err != nil {
		j.logger.ErrorContext(ctx, "参照されていない画像の取得に失敗した", "error", err)
		return
	}
	if len(orphans) == 0 {
		return
	}

	deleted := 0
	for _, o := range orphans {
		// **S3 を先に消す。** 順序を逆にすると、行が消えたあとに
		// S3 の削除が失敗した場合、そのオブジェクトを辿る手段が
		// 永久に無くなる。この順なら次の回でもう一度拾える。
		if err := j.objects.Delete(ctx, o.Keys...); err != nil {
			j.logger.ErrorContext(ctx, "参照されていない画像を S3 から消せなかった",
				"media_id", o.MediaID, "error", err)
			continue
		}
		if err := j.store.DeleteMedia(ctx, o.MediaID); err != nil {
			j.logger.ErrorContext(ctx, "参照されていない画像の行を消せなかった",
				"media_id", o.MediaID, "error", err)
			continue
		}
		deleted++
	}
	j.logger.InfoContext(ctx, "参照されていない画像を掃除した",
		"deleted", deleted, "found", len(orphans))
}
