package domain

// User は利用者を表す。
//
// **パスワードのハッシュを持たない。** 認証以外の場所へ渡っても
// 秘密が付いて回らないようにするためである。ログイン時のみ
// store が別の型で取り出す。
type User struct {
	ID          uint64
	Handle      string
	Email       string
	DisplayName string
	Bio         *string
}

// Credentials はログインの照合にのみ使う。
//
// User と分けているのは、パスワードのハッシュが認証以外の経路へ
// 流れないようにするためである。User を JSON 化しても秘密は付いてこない。
type Credentials struct {
	User         User
	PasswordHash string
}
