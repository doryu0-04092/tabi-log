package domain

import "time"

// Post は旅行の記録の投稿を表す。
type Post struct {
	ID     uint64
	Author User
	Body   string

	// Prefecture は必須。自由入力ではなく47件のマスタから選ぶ。
	// 自由入力にすると「北海道」「ほっかいどう」「Hokkaido」が分散し、
	// 地域での絞り込みという中核機能が成立しなくなる。
	Prefecture Prefecture

	// SpotName は「道の駅○○」など。全文検索の対象には含めるが、
	// 絞り込みの軸にはしない。
	SpotName *string

	// VisitedOn は訪問日。**投稿日（CreatedAt）とは別の軸である。**
	// 旅行から帰ったあとにまとめて投稿するのが自然な使われ方のため。
	VisitedOn time.Time

	Media []PostMedia
	Tags  []string

	// LikeCount / CommentCount はカウンタ列から読む。
	// フィードで20件それぞれに集計を撃つと N+1 の温床になるため、
	// いいね・コメントの登録と同一トランザクションで増減させている。
	LikeCount    int
	CommentCount int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PostMedia は投稿に紐づいた画像1枚を表す。
type PostMedia struct {
	ID      uint64
	AltText string
	Width   int
	Height  int

	// ThumbURL / MediumURL は表示用の変換物への URL。
	// **原本は配らない。** 原本には EXIF 除去前の情報が含まれる可能性があり、
	// 大きさの面でも一覧に適さない。
	ThumbURL  string
	MediumURL string
}

// Comment は投稿へのコメントを表す。
//
// 返信ツリーは作らないため親コメントへの参照は持たない
// （要件定義書 3.2 の対象外）。
type Comment struct {
	ID        uint64
	Author    User
	Body      string
	CreatedAt time.Time
}

// Notification は利用者あての通知。
//
// 契機（いいね・コメント・フォロー）ごとに埋まる項目が違うため、
// 使わない項目はポインタで「無い」を表せるようにしている。
type Notification struct {
	ID    uint64
	Kind  string
	Actor User
	// PostID は like と comment に入る。画面はこれで投稿へ飛ぶ。
	PostID *uint64
	// CommentBody は comment に入る。**一覧に本文の頭を出すために持たせる。**
	// 通知を開くたびにコメントを取りに行くと、20件で20往復になる。
	CommentBody *string
	IsRead      bool
	CreatedAt   time.Time
}
