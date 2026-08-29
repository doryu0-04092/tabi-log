package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

/*
store の振る舞いを実際の MySQL に対して確かめる。

**ここで見るのは「Go の呼び出しが通ること」ではなく「SQL が意図どおりか」である。**
偽の実装を相手にすると、外部キーの連鎖・一意制約・カウンタの更新といった
データベース側が担っている部分が丸ごと検証から抜ける。
*/

func Test投稿を作ると画像とタグが同時に保存される(t *testing.T) {
	db := newDB(t)
	userID := createUser(t, db, "poster")

	mediaID := createProcessedMedia(t, db, userID, "originals/1/a.png")
	postID, err := NewPostStore(db).CreatePost(t.Context(), CreatePostInput{
		UserID:         userID,
		Body:           "函館の朝市",
		PrefectureCode: "01",
		Tags:           []string{"海鮮", "朝市"},
		Media:          []MediaAttachment{{MediaID: mediaID}},
	})
	if err != nil {
		t.Fatalf("投稿を作れない: %v", err)
	}

	if n := countRows(t, db, "media", "post_id = ? AND sort_order = 0", postID); n != 1 {
		t.Errorf("画像が投稿に紐づいていない: %d件", n)
	}
	if n := countRows(t, db, "post_tags", "post_id = ?", postID); n != 2 {
		t.Errorf("タグの紐づけが %d件。2件のはず", n)
	}
	if n := countRows(t, db, "tags", ""); n != 2 {
		t.Errorf("タグが %d件。2件のはず", n)
	}

	post, err := NewPostStore(db).GetPost(t.Context(), postID, fakeSigner{}, time.Minute)
	if err != nil {
		t.Fatalf("投稿を取得できない: %v", err)
	}
	if post.Body != "函館の朝市" {
		t.Errorf("本文が %q", post.Body)
	}
	if len(post.Media) != 1 {
		t.Fatalf("画像が %d枚", len(post.Media))
	}
	if post.Prefecture.Name != "北海道" {
		t.Errorf("都道府県が %q", post.Prefecture.Name)
	}
}

// 他人の画像・未処理の画像は使えない。
// **区別せず1つのエラーにしている**（他人の画像 ID の総当たりを防ぐため）。
func Test他人の画像は投稿に使えない(t *testing.T) {
	db := newDB(t)
	owner := createUser(t, db, "owner")
	other := createUser(t, db, "other")

	mediaID := createProcessedMedia(t, db, owner, "originals/owner/a.png")

	_, err := NewPostStore(db).CreatePost(t.Context(), CreatePostInput{
		UserID:         other,
		Body:           "他人の画像で投稿する",
		PrefectureCode: "01",
		Media:          []MediaAttachment{{MediaID: mediaID}},
	})
	if !errors.Is(err, ErrMediaNotUsable) {
		t.Fatalf("ErrMediaNotUsable のはずが %v", err)
	}

	// **投稿ごと巻き戻っていること。** 画像だけ失敗して本体が残ると、
	// 画像の無い投稿ができる。
	if n := countRows(t, db, "posts", ""); n != 0 {
		t.Errorf("投稿が %d件残っている", n)
	}
}

func Testいいねはカウンタと通知を同時に更新し二重に押しても増えない(t *testing.T) {
	db := newDB(t)
	author := createUser(t, db, "author")
	liker := createUser(t, db, "liker")
	postID := createPost(t, db, author, "いいねされる投稿")

	reactions := NewReactionStore(db)
	if err := reactions.Like(t.Context(), liker, postID); err != nil {
		t.Fatalf("いいねできない: %v", err)
	}

	if n := countRows(t, db, "posts", "id = ? AND like_count = 1", postID); n != 1 {
		t.Error("like_count が 1 になっていない")
	}
	if n := countRows(t, db, "notifications", "user_id = ? AND type = 'like'", author); n != 1 {
		t.Errorf("通知が %d件。1件のはず", n)
	}

	// **冪等であること。** 通信のやり直しや二重タップで 409 を返すと、
	// 利用者から見て「押せているのに失敗する」状態になる。
	if err := reactions.Like(t.Context(), liker, postID); err != nil {
		t.Fatalf("2回目のいいねで失敗した: %v", err)
	}
	if n := countRows(t, db, "posts", "id = ? AND like_count = 1", postID); n != 1 {
		t.Error("2回押すと like_count が増えている")
	}
	if n := countRows(t, db, "notifications", "user_id = ?", author); n != 1 {
		t.Errorf("2回押すと通知が %d件に増えている", n)
	}

	if err := reactions.Unlike(t.Context(), liker, postID); err != nil {
		t.Fatalf("いいねを外せない: %v", err)
	}
	if n := countRows(t, db, "posts", "id = ? AND like_count = 0", postID); n != 1 {
		t.Error("外したのに like_count が 0 になっていない")
	}

	// 外した状態でもう一度外しても負にならないこと。
	if err := reactions.Unlike(t.Context(), liker, postID); err != nil {
		t.Fatalf("2回目の解除で失敗した: %v", err)
	}
	if n := countRows(t, db, "posts", "id = ? AND like_count = 0", postID); n != 1 {
		t.Error("like_count が負になっている")
	}
}

func Test自分の投稿へのいいねでは通知を作らない(t *testing.T) {
	db := newDB(t)
	author := createUser(t, db, "selfliker")
	postID := createPost(t, db, author, "自分の投稿")

	if err := NewReactionStore(db).Like(t.Context(), author, postID); err != nil {
		t.Fatalf("いいねできない: %v", err)
	}
	if n := countRows(t, db, "notifications", ""); n != 0 {
		t.Errorf("自分への通知が %d件作られている", n)
	}
}

func Testコメントはカウンタと通知を同時に更新する(t *testing.T) {
	db := newDB(t)
	author := createUser(t, db, "cauthor")
	commenter := createUser(t, db, "commenter")
	postID := createPost(t, db, author, "コメントされる投稿")

	commentID, err := NewReactionStore(db).CreateComment(t.Context(), commenter, postID, "行ってみたい")
	if err != nil {
		t.Fatalf("コメントできない: %v", err)
	}

	if n := countRows(t, db, "posts", "id = ? AND comment_count = 1", postID); n != 1 {
		t.Error("comment_count が 1 になっていない")
	}
	if n := countRows(t, db, "notifications", "user_id = ? AND type = 'comment' AND comment_id = ?", author, commentID); n != 1 {
		t.Error("コメントの通知が作られていない")
	}

	if err := NewReactionStore(db).DeleteComment(t.Context(), commentID, postID); err != nil {
		t.Fatalf("コメントを消せない: %v", err)
	}
	if n := countRows(t, db, "posts", "id = ? AND comment_count = 0", postID); n != 1 {
		t.Error("消したのに comment_count が戻っていない")
	}
}

/*
退会したときに、**他人の投稿に付けたいいね・コメントの件数も戻ること。**

これは実際に起きていた不具合の再現である。退会で likes / comments の行は
消えていたが、`posts` のカウンタ列は投稿の所有者側しか見ておらず、
**他人の投稿に付けた分だけカウンタが実体より多いまま残っていた。**
画面には投稿の一覧からカウンタを出しているため、
「いいね1件と出ているのに、いいねした人が誰もいない」状態になる。

単体テストでは見つからない。カウンタと行が別々に管理されていること自体が
原因であり、その2つの整合はデータベースの中でしか確認できない。
*/
func Test退会すると他人の投稿に付けた反応の件数も戻る(t *testing.T) {
	db := newDB(t)
	author := createUser(t, db, "remaining")
	leaver := createUser(t, db, "leaver")
	postID := createPost(t, db, author, "残る人の投稿")

	reactions := NewReactionStore(db)
	if err := reactions.Like(t.Context(), leaver, postID); err != nil {
		t.Fatalf("いいねできない: %v", err)
	}
	if _, err := reactions.CreateComment(t.Context(), leaver, postID, "行きました"); err != nil {
		t.Fatalf("コメントできない: %v", err)
	}
	if n := countRows(t, db, "posts", "id = ? AND like_count = 1 AND comment_count = 1", postID); n != 1 {
		t.Fatal("前提が崩れている。反応が記録されていない")
	}

	if _, err := NewAccountStore(db).DeleteAccount(t.Context(), leaver, time.Now()); err != nil {
		t.Fatalf("退会できない: %v", err)
	}

	if n := countRows(t, db, "likes", ""); n != 0 {
		t.Errorf("いいねの行が %d件残っている", n)
	}
	if n := countRows(t, db, "comments", ""); n != 0 {
		t.Errorf("コメントの行が %d件残っている", n)
	}
	if n := countRows(t, db, "posts", "id = ? AND like_count = 0 AND comment_count = 0", postID); n != 1 {
		var likes, comments int
		_ = db.QueryRowContext(t.Context(),
			"SELECT like_count, comment_count FROM posts WHERE id = ?", postID).Scan(&likes, &comments)
		t.Errorf("カウンタが実体と合っていない: like_count=%d comment_count=%d（どちらも0のはず）", likes, comments)
	}
}

func Test退会すると自分の投稿と画像の鍵が返る(t *testing.T) {
	db := newDB(t)
	leaver := createUser(t, db, "leaver2")
	createPost(t, db, leaver, "消える投稿")

	keys, err := NewAccountStore(db).DeleteAccount(t.Context(), leaver, time.Now())
	if err != nil {
		t.Fatalf("退会できない: %v", err)
	}

	// **S3 は外部キーの連鎖では消えない。** 呼び出し側が消すための鍵が要る。
	if len(keys) == 0 {
		t.Error("削除すべき S3 の鍵が返っていない")
	}
	if n := countRows(t, db, "posts", ""); n != 0 {
		t.Errorf("投稿が %d件残っている", n)
	}
	if n := countRows(t, db, "media", ""); n != 0 {
		t.Errorf("画像が %d件残っている", n)
	}
	// **利用者の行は残す（論理削除）。** 他人の画面に出ていた表示名や
	// 参照を壊さないためである。代わりに、
	// ログインできない状態にしてあることを確かめる。
	if n := countRows(t, db, "users", "id = ? AND deleted_at IS NOT NULL", leaver); n != 1 {
		t.Error("退会の印が付いていない")
	}
	if n := countRows(t, db, "users", "id = ? AND password_hash = 'deleted'", leaver); n != 1 {
		t.Error("パスワードのハッシュが使える値のまま残っている")
	}
	// **メールアドレスは置き換える。** UNIQUE 制約があるため、
	// 置き換えないと本人が同じアドレスで登録し直せない。
	if n := countRows(t, db, "users", "id = ? AND email LIKE 'deleted-%@invalid.example'", leaver); n != 1 {
		t.Error("メールアドレスが元のまま残っている")
	}
}

func Testカーソルでたどると重複も抜けも無い(t *testing.T) {
	db := newDB(t)
	userID := createUser(t, db, "paged")

	const total = 25
	for i := range total {
		createPost(t, db, userID, fmt.Sprintf("ページ送り %02d", i))
	}

	posts := NewPostStore(db)
	seen := map[uint64]bool{}
	cursor := uint64(0)
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatal("ページが終わらない。カーソルが進んでいない可能性がある")
		}
		batch, next, err := posts.ListFeed(t.Context(), cursor, 10, fakeSigner{}, time.Minute)
		if err != nil {
			t.Fatalf("フィードを取得できない: %v", err)
		}
		for _, p := range batch {
			if seen[p.ID] {
				t.Fatalf("投稿 %d が2回出てきた", p.ID)
			}
			seen[p.ID] = true
		}
		if next == 0 {
			break
		}
		cursor = next
	}

	if len(seen) != total {
		t.Errorf("%d件しか取れていない。%d件あるはず", len(seen), total)
	}
}

func Test旅行履歴には訪問日の無い投稿が出ない(t *testing.T) {
	db := newDB(t)
	userID := createUser(t, db, "traveler")

	visited := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	createPost(t, db, userID, "訪問日あり", func(in *CreatePostInput) { in.VisitedOn = &visited })
	createPost(t, db, userID, "訪問日なし")

	// カーソルの起点は「いちばん新しい」を表す最大値。
	cursor := TravelCursor{VisitedOn: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), ID: 1 << 62}
	travels, _, err := NewPostStore(db).ListUserTravels(t.Context(), userID, cursor, 10, fakeSigner{}, time.Minute)
	if err != nil {
		t.Fatalf("旅行履歴を取得できない: %v", err)
	}

	if len(travels) != 1 {
		t.Fatalf("%d件返った。訪問日のある1件だけのはず", len(travels))
	}
	if travels[0].Body != "訪問日あり" {
		t.Errorf("返ったのが %q", travels[0].Body)
	}
}

/*
リフレッシュトークンのローテーションと盗用検知。

**ここは実際の行の状態でしか確認できない。** 「失効済みだが猶予内」と
「失効済みで猶予外」の違いは、`revoked_at` と `replaced_by` という
2つの列の組み合わせで表している。
*/
func Testリフレッシュトークンは猶予内の再提示を許し猶予外では全失効する(t *testing.T) {
	db := newDB(t)
	userID := createUser(t, db, "refresher")

	auth := NewAuthStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(7 * 24 * time.Hour)

	if err := auth.CreateRefreshToken(t.Context(), userID, "hash-1", expires); err != nil {
		t.Fatalf("トークンを作れない: %v", err)
	}

	// 正規のローテーション。
	if _, err := auth.RotateRefreshToken(t.Context(), "hash-1", "hash-2", expires, now, 10*time.Second); err != nil {
		t.Fatalf("ローテーションできない: %v", err)
	}

	// **タブを2枚開いた利用者は同じトークンで同時にリフレッシュする。**
	// 猶予内であれば、後発も通らなければならない。
	got, err := auth.RotateRefreshToken(t.Context(), "hash-1", "hash-3", expires, now.Add(3*time.Second), 10*time.Second)
	if err != nil {
		t.Fatalf("猶予内の再提示が拒否された: %v", err)
	}
	if got != userID {
		t.Errorf("利用者 %d が返った。%d のはず", got, userID)
	}

	// 猶予を過ぎた再提示は盗用とみなす。
	_, err = auth.RotateRefreshToken(t.Context(), "hash-1", "hash-4", expires, now.Add(time.Minute), 10*time.Second)
	if !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("ErrRefreshTokenReused のはずが %v", err)
	}

	// **そのユーザーの全トークンが失効していること。**
	// どちらが正規の利用者か判定できないため、両方を無効にする。
	if n := countRows(t, db, "refresh_tokens", "user_id = ? AND revoked_at IS NULL", userID); n != 0 {
		t.Errorf("失効していないトークンが %d件残っている", n)
	}
}

func Testフォローすると行と通知ができ解除で消える(t *testing.T) {
	db := newDB(t)
	follower := createUser(t, db, "follower")
	followee := createUser(t, db, "followee")

	follows := NewFollowStore(db)
	if err := follows.Follow(t.Context(), follower, followee); err != nil {
		t.Fatalf("フォローできない: %v", err)
	}

	// 冪等であること。二重タップで失敗させない。
	if err := follows.Follow(t.Context(), follower, followee); err != nil {
		t.Fatalf("2回目のフォローで失敗した: %v", err)
	}
	if n := countRows(t, db, "follows", ""); n != 1 {
		t.Errorf("フォローの行が %d件。1件のはず", n)
	}
	if n := countRows(t, db, "notifications", "user_id = ? AND type = 'follow'", followee); n != 1 {
		t.Errorf("フォローの通知が %d件。1件のはず", n)
	}

	if err := follows.Unfollow(t.Context(), follower, followee); err != nil {
		t.Fatalf("解除できない: %v", err)
	}
	if n := countRows(t, db, "follows", ""); n != 0 {
		t.Errorf("解除したのに %d件残っている", n)
	}
}

func Test通知は既読にすると未読数から外れる(t *testing.T) {
	db := newDB(t)
	author := createUser(t, db, "notified")
	actor := createUser(t, db, "actor")
	postID := createPost(t, db, author, "通知のもとになる投稿")

	if err := NewReactionStore(db).Like(t.Context(), actor, postID); err != nil {
		t.Fatalf("いいねできない: %v", err)
	}

	notifications := NewNotificationStore(db)
	unread, err := notifications.UnreadCount(t.Context(), author)
	if err != nil {
		t.Fatalf("未読数を取得できない: %v", err)
	}
	if unread != 1 {
		t.Fatalf("未読が %d件。1件のはず", unread)
	}

	if err := notifications.MarkAllRead(t.Context(), author, time.Now()); err != nil {
		t.Fatalf("既読にできない: %v", err)
	}
	unread, err = notifications.UnreadCount(t.Context(), author)
	if err != nil {
		t.Fatalf("未読数を取得できない: %v", err)
	}
	if unread != 0 {
		t.Errorf("既読にしたのに未読が %d件", unread)
	}
}

// **他人あての通知を ID だけで既読にできてはいけない。**
// 権限の担保を WHERE 句に入れてある。
func Test他人あての通知は既読にできない(t *testing.T) {
	db := newDB(t)
	author := createUser(t, db, "victim")
	actor := createUser(t, db, "attacker")
	postID := createPost(t, db, author, "投稿")

	if err := NewReactionStore(db).Like(t.Context(), actor, postID); err != nil {
		t.Fatalf("いいねできない: %v", err)
	}

	list, _, err := NewNotificationStore(db).List(t.Context(), author, 0, 10)
	if err != nil {
		t.Fatalf("通知を取得できない: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("通知が %d件", len(list))
	}

	err = NewNotificationStore(db).MarkRead(t.Context(), list[0].ID, actor, time.Now())
	if err == nil {
		t.Fatal("他人あての通知を既読にできてしまった")
	}
	if n := countRows(t, db, "notifications", "id = ? AND read_at IS NULL", list[0].ID); n != 1 {
		t.Error("通知が既読になっている")
	}
}
