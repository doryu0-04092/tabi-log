package janitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

/*
掃除の順序と、失敗したときの振る舞い。

**S3 を先に消す。** 順序が逆だと、行が消えたあとに S3 の削除が失敗した
場合、そのオブジェクトを辿る手段が永久に無くなる。この順なら次の回で
もう一度拾える。

`docs/audit-2026-08-31.md` M4 / M6。
*/

type fakeStore struct {
	tokens   int64
	tokenErr error

	orphans   []store.OrphanMedia
	orphanErr error

	deleted    []uint64
	deleteErr  error
	pending    []store.PendingDeletion
	pendingErr error
	forgotten  []uint64
	attempted  []uint64

	tokenSince time.Time
	mediaSince time.Time
}

func (f *fakeStore) DeleteExpiredRefreshTokens(_ context.Context, before time.Time, _ int32) (int64, error) {
	f.tokenSince = before
	return f.tokens, f.tokenErr
}

func (f *fakeStore) ListOrphanMedia(_ context.Context, before time.Time, _ int32) ([]store.OrphanMedia, error) {
	f.mediaSince = before
	return f.orphans, f.orphanErr
}

func (f *fakeStore) DeleteMedia(_ context.Context, id uint64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	return nil
}

// pending は削除待ちの控え。**S3 から消せたときだけ忘れる。**
func (f *fakeStore) ListPendingDeletions(_ context.Context, _ int32) ([]store.PendingDeletion, error) {
	return f.pending, f.pendingErr
}

func (f *fakeStore) ForgetPendingDeletion(_ context.Context, id uint64) error {
	f.forgotten = append(f.forgotten, id)
	return nil
}

func (f *fakeStore) RecordDeletionAttempt(_ context.Context, id uint64) error {
	f.attempted = append(f.attempted, id)
	return nil
}

type fakeObjects struct {
	deleted []string
	err     error
}

func (f *fakeObjects) Delete(_ context.Context, keys ...string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, keys...)
	return nil
}

func newJanitor(s Store, o ObjectDeleter) *Janitor {
	j := New(s, o, DefaultConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	j.now = func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }
	return j
}

func Test参照されていない画像は原本も変換物も消える(t *testing.T) {
	s := &fakeStore{orphans: []store.OrphanMedia{
		{MediaID: 1, Keys: []string{"originals/1.jpg", "variants/1_thumb.jpg", "variants/1_medium.jpg"}},
	}}
	o := &fakeObjects{}

	newJanitor(s, o).RunOnce(context.Background())

	if len(o.deleted) != 3 {
		t.Errorf("S3 から消したのが %d 件だった。3件を期待した: %v", len(o.deleted), o.deleted)
	}
	if len(s.deleted) != 1 || s.deleted[0] != 1 {
		t.Errorf("行を消していない: %v", s.deleted)
	}
}

// **S3 の削除に失敗したら行を残す。** 消してしまうと、そのオブジェクトを
// 辿る手段が永久に無くなる。
func Test画像をS3から消せなければ行を残す(t *testing.T) {
	s := &fakeStore{orphans: []store.OrphanMedia{
		{MediaID: 1, Keys: []string{"originals/1.jpg"}},
	}}
	o := &fakeObjects{err: errors.New("S3 に届かない")}

	newJanitor(s, o).RunOnce(context.Background())

	if len(s.deleted) != 0 {
		t.Errorf("S3 を消せていないのに行を消した: %v。次の回で拾えなくなる", s.deleted)
	}
}

// トークンの掃除が失敗しても、画像の掃除は続ける。
// **片方の失敗でもう片方が止まると、溜まり方が偏る。**
func Test片方が失敗してももう片方は続ける(t *testing.T) {
	s := &fakeStore{
		tokenErr: errors.New("DB に届かない"),
		orphans:  []store.OrphanMedia{{MediaID: 1, Keys: []string{"originals/1.jpg"}}},
	}
	o := &fakeObjects{}

	newJanitor(s, o).RunOnce(context.Background())

	if len(s.deleted) != 1 {
		t.Errorf("トークンの失敗で画像の掃除まで止まった: %v", s.deleted)
	}
}

// 猶予より新しいものは対象にしない。
// **アップロード中の画像を消すと、Lambda が「知らない画像だ」と失敗する。**
func Test猶予をさかのぼった時点より前だけを対象にする(t *testing.T) {
	s := &fakeStore{}
	j := newJanitor(s, &fakeObjects{})
	j.RunOnce(context.Background())

	now := j.now()
	if want := now.Add(-DefaultConfig().MediaRetention); !s.mediaSince.Equal(want) {
		t.Errorf("画像の基準が %v だった。%v を期待した", s.mediaSince, want)
	}
	if want := now.Add(-DefaultConfig().TokenRetention); !s.tokenSince.Equal(want) {
		t.Errorf("トークンの基準が %v だった。%v を期待した", s.tokenSince, want)
	}
}

// 猶予は S3 のライフサイクル（既定7日）より長くなければならない。
// **短いと、まだ使われうる画像の行を先に消してしまう。**
func Test画像の猶予はS3のライフサイクルより長い(t *testing.T) {
	const s3Lifecycle = 7 * 24 * time.Hour
	if DefaultConfig().MediaRetention <= s3Lifecycle {
		t.Errorf("画像の猶予が %v。S3 のライフサイクル %v より長くすること",
			DefaultConfig().MediaRetention, s3Lifecycle)
	}
}

// 消し損ねたオブジェクトは掃除で消し直す。
// **控えを消すのは S3 から消せたときだけ。**
func Test消し損ねたオブジェクトは掃除で消し直す(t *testing.T) {
	s := &fakeStore{pending: []store.PendingDeletion{{ID: 3, S3Key: "variants/9_thumb.jpg"}}}
	o := &fakeObjects{}

	newJanitor(s, o).RunOnce(context.Background())

	if len(o.deleted) != 1 || o.deleted[0] != "variants/9_thumb.jpg" {
		t.Errorf("S3 から消していない: %v", o.deleted)
	}
	if len(s.forgotten) != 1 || s.forgotten[0] != 3 {
		t.Errorf("控えを消していない: %v", s.forgotten)
	}
}

// **消せなかったら控えを残す。** 忘れると鍵を辿る手段が永久に無くなる。
func Test消し直しに失敗したら控えを残す(t *testing.T) {
	s := &fakeStore{pending: []store.PendingDeletion{{ID: 3, S3Key: "variants/9_thumb.jpg"}}}
	o := &fakeObjects{err: errors.New("S3 に届かない")}

	newJanitor(s, o).RunOnce(context.Background())

	if len(s.forgotten) != 0 {
		t.Errorf("消せていないのに控えを消した: %v。鍵を辿れなくなる", s.forgotten)
	}
	if len(s.attempted) != 1 {
		t.Errorf("失敗を数えていない: %v。何度も失敗するものを見分けられない", s.attempted)
	}
}
