package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doryu0-04092/tabi-log/backend/internal/api/gen"
	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"
)

// refreshCookieName はリフレッシュトークンを載せる Cookie 名。
const refreshCookieName = "tabilog_refresh"

// refreshCookiePath は Cookie を送る範囲。
//
// 投稿や検索といった他の API リクエストには乗らないようにしつつ、
// **ログアウトでも Cookie を読めるようにするため `/api/auth` までとする**。
// `/api/auth/refresh` まで絞ると、ログアウト時にサーバー側で失効させられない。
const refreshCookiePath = "/api/auth"

// sessionHintCookieName は「セッションがありそうだ」ということだけを示す印。
//
// **秘密を含まない。値は常に "1" である。**
//
// これが無いと、画面は起動のたびに /api/auth/refresh を叩くことになる。
// 未ログインの人にとっては必ず 401 が返る無駄な往復であり、
// 最初の描画が1往復ぶん遅れるうえ、ブラウザのコンソールにエラーが残る。
//
// **HttpOnly にしない。** JavaScript から読めることが目的である。
// 読めても分かるのは「リフレッシュトークンを持っているらしい」ことだけで、
// トークンそのものは HttpOnly の側にあり読めない。
//
// Path を "/" にしているのは、どの画面からでも読めるようにするためである。
// リフレッシュトークン本体（Path=/api/auth）とは送られる範囲が違ってよい。
// **印の寿命はトークンと揃える。** ずれると、印はあるのに
// トークンが無い（無駄な 401 が戻る）か、その逆（ログアウトして見える）になる。
const sessionHintCookieName = "tabilog_session"

// AuthRepository は認証に必要な永続化操作を表す。
//
// インターフェースを使う側（この層）で宣言している。
// ハンドラのテストにデータベースを要らなくするためである。
type AuthRepository interface {
	CreateUser(ctx context.Context, handle, email, passwordHash, displayName string) (domain.User, error)
	FindCredentialsByEmail(ctx context.Context, email string) (domain.Credentials, error)
	FindUserByID(ctx context.Context, id uint64) (domain.User, error)
	CreateRefreshToken(ctx context.Context, userID uint64, hash string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, presentedHash, newHash string, newExpiresAt, now time.Time, grace time.Duration) (uint64, error)
	RevokeRefreshTokenByHash(ctx context.Context, hash string, now time.Time) error
}

// AuthOptions はハンドラの振る舞いを決める設定。
type AuthOptions struct {
	RefreshTTL   time.Duration
	RefreshGrace time.Duration
	CookieSecure bool
	// TrustProxyHeaders は X-Forwarded-For を信用するかどうか。
	//
	// **ALB や CloudFront の背後では必ず true にする。** false のままだと
	// r.RemoteAddr がロードバランサのアドレスになり、全利用者が同じ鍵で
	// 数えられて、レート制限が事実上「全体で N 回」になってしまう。
	//
	// 逆に、プロキシの背後でないのに true にすると、
	// **攻撃者がヘッダーを自由に詐称してレート制限を回避できる。**
	// どちらの向きにも誤ると壊れるため、環境ごとに明示的に設定する。
	TrustProxyHeaders bool

	// TrustedProxyHops は X-Forwarded-For の末尾から数えて何個目を
	// 「実際の接続元」とみなすか。アプリの手前にある信頼できるプロキシのうち、
	// 自分自身を除いた段数である。CloudFront → ALB → アプリ なら 1。ALB直結なら 0。
	//
	// **左端を採ってはいけない。** X-Forwarded-For の左端はクライアントが
	// 自分で名乗った値であり、リクエストごとに詐称できる。左端を鍵にすると
	// 鍵が毎回変わり、レート制限が事実上無効になる。
	//
	// プロキシは受け取った値の末尾に、自分が観測した接続元を追記する。
	// したがって信頼できるのは「自分より手前の段が書いた分」だけである。
	//
	//	クライアントが詐称   : 1.2.3.4
	//	CloudFront が追記     : 1.2.3.4, 203.0.113.9
	//	ALB が追記            : 1.2.3.4, 203.0.113.9, 130.176.0.1
	//	                                 ^^^^^^^^^^^^ これが利用者
	//
	// **構成の段数を変えたら必ずこの値も変える。** 大きすぎると詐称できる領域に届き、
	// 小さすぎると全員が同じプロキシのIPとして1つの鍵に潰れる。
	TrustedProxyHops int
}

type authHandler struct {
	repo    AuthRepository
	issuer  auth.TokenIssuer
	opts    AuthOptions
	logger  *slog.Logger
	byIP    *RateLimiter
	byEmail *RateLimiter
	// cdn は画像配信の署名付き Cookie を発行する。
	// **nil を許す**（ローカルと LocalStack には CloudFront が無い）。
	cdn      *cdnCookieIssuer
	now      func() time.Time
	newToken func() (string, string, error)
}

// ---------------------------------------------------------------------------
// サインアップ
// ---------------------------------------------------------------------------

func (h *authHandler) Signup(w http.ResponseWriter, r *http.Request) {
	if !h.byIP.Allow("signup:" + h.clientIP(r)) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "試行回数が多すぎます。しばらく待ってからお試しください")
		return
	}

	var req gen.SignupRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := strings.TrimSpace(req.Email)
	handle := strings.TrimSpace(req.Handle)
	displayName := strings.TrimSpace(req.DisplayName)

	if msg := validateSignup(email, handle, displayName, req.Password); msg != "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", msg)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.internalError(w, r, "パスワードのハッシュ化に失敗した", err)
		return
	}

	user, err := h.repo.CreateUser(r.Context(), handle, email, passwordHash, displayName)
	switch {
	case errors.Is(err, store.ErrEmailTaken):
		writeError(w, r, http.StatusConflict, "email_taken", "そのメールアドレスは既に使われています")
		return
	case errors.Is(err, store.ErrHandleTaken):
		writeError(w, r, http.StatusConflict, "handle_taken", "そのハンドルは既に使われています")
		return
	case err != nil:
		h.internalError(w, r, "利用者の作成に失敗した", err)
		return
	}

	h.respondWithSession(w, r, user, http.StatusCreated)
}

// ---------------------------------------------------------------------------
// ログイン
// ---------------------------------------------------------------------------

func (h *authHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req gen.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)

	// IP とアカウントの両方で数える。
	//
	// IP だけだと、分散した発信元からの1アカウント狙い撃ちを止められない。
	// アカウントだけだと、**攻撃者が他人のアドレスを送り続けることで
	// その利用者をログインできなくできる**（サービス拒否になる）。
	// 両方を独立に数えることで、どちらの形も抑える。
	//
	// **鍵は小文字に揃える。** users.email の照合順序は utf8mb4_0900_ai_ci で、
	// DB は大文字小文字を区別しない。生のまま数えると、
	// User@example.com と user@example.com が別々に数えられ、
	// **大文字小文字を変えるだけで試行回数を何倍にもできる。**
	if !h.byIP.Allow("login:"+h.clientIP(r)) || !h.byEmail.Allow("login:"+strings.ToLower(email)) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "試行回数が多すぎます。しばらく待ってからお試しください")
		return
	}

	creds, err := h.repo.FindCredentialsByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrUserNotFound) {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	// **利用者が見つからない場合もハッシュの比較を行う。**
	//
	// 見つからない時点で即座に返すと、応答時間が明らかに短くなり、
	// 「そのメールアドレスは登録されているか」を外部から判定できてしまう。
	// bcrypt の比較には数十ミリ秒かかるため、この差は計測できる大きさになる。
	hash := creds.PasswordHash
	if hash == "" {
		hash = auth.DummyPasswordHash
	}
	ok := auth.VerifyPassword(hash, req.Password)

	if err != nil || !ok {
		// **失敗の理由を区別しない。** 「登録されていない」と「パスワードが違う」を
		// 分けて返すと、どのアドレスが登録済みかを総当たりで調べられる。
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials",
			"メールアドレスまたはパスワードが正しくありません")
		return
	}

	// 正しく使えている利用者が上限に近づいたままにならないよう、成功時に消す。
	h.byEmail.Reset("login:" + email)

	h.respondWithSession(w, r, creds.User, http.StatusOK)
}

// ---------------------------------------------------------------------------
// トークンの再発行
// ---------------------------------------------------------------------------

func (h *authHandler) Refresh(w http.ResponseWriter, r *http.Request, _ gen.RefreshParams) {
	if !requireCSRFHeader(w, r) {
		return
	}

	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		// **印だけが残っている状態を、ここで解消する。**
		// 印はあるのにトークンが無いと、画面は開くたびに
		// ここへ問い合わせに来続けることになる。
		h.clearSessionHintCookie(w)
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	now := h.now()
	newToken, newHash, err := h.newToken()
	if err != nil {
		h.internalError(w, r, "リフレッシュトークンの生成に失敗した", err)
		return
	}
	newExpiresAt := now.Add(h.opts.RefreshTTL)

	userID, err := h.repo.RotateRefreshToken(
		r.Context(), auth.HashRefreshToken(cookie.Value), newHash, newExpiresAt, now, h.opts.RefreshGrace)

	switch {
	case errors.Is(err, store.ErrRefreshTokenReused):
		// 盗用の疑いは記録に残す。利用者から見ると突然のログアウトなので、
		// 問い合わせがあったときに何が起きたかを説明できる必要がある。
		h.logger.WarnContext(r.Context(), "失効済みリフレッシュトークンの再提示を検知し、全トークンを失効させた",
			slog.String("request_id", RequestIDFrom(r.Context())),
		)
		h.clearRefreshCookie(w)
		writeError(w, r, http.StatusUnauthorized, "token_reuse_detected",
			"安全のため全てのセッションを終了しました。再度ログインしてください")
		return

	case errors.Is(err, store.ErrRefreshTokenNotFound), errors.Is(err, store.ErrRefreshTokenExpired):
		h.clearRefreshCookie(w)
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return

	case err != nil:
		h.internalError(w, r, "リフレッシュトークンのローテーションに失敗した", err)
		return
	}

	user, err := h.repo.FindUserByID(r.Context(), userID)
	if err != nil {
		// トークンは有効だが利用者が退会している場合もここに来る。
		h.clearRefreshCookie(w)
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	h.setRefreshCookie(w, r, newToken, newExpiresAt)
	h.writeAuthResponse(w, r, user, http.StatusOK)
}

// ---------------------------------------------------------------------------
// ログアウト
// ---------------------------------------------------------------------------

func (h *authHandler) Logout(w http.ResponseWriter, r *http.Request, _ gen.LogoutParams) {
	if !requireCSRFHeader(w, r) {
		return
	}

	// Cookie が無くても成功として扱う（冪等）。
	// 「既にログアウトしている」ことは失敗ではない。
	if cookie, err := r.Cookie(refreshCookieName); err == nil && cookie.Value != "" {
		if err := h.repo.RevokeRefreshTokenByHash(r.Context(), auth.HashRefreshToken(cookie.Value), h.now()); err != nil {
			h.internalError(w, r, "リフレッシュトークンの失効に失敗した", err)
			return
		}
	}

	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// ログイン中の利用者
// ---------------------------------------------------------------------------

func (h *authHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		// WithAuthentication を通っていれば必ず入っている。
		// ここに来るのは経路の設定ミスであり、認証をすり抜けさせない。
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}

	user, err := h.repo.FindUserByID(r.Context(), userID)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		return
	}
	if err != nil {
		h.internalError(w, r, "利用者の取得に失敗した", err)
		return
	}

	writeJSON(w, r, http.StatusOK, toAPIUser(user))
}

// ---------------------------------------------------------------------------
// 共通処理
// ---------------------------------------------------------------------------

// respondWithSession はリフレッシュトークンを新規に発行してセッションを開始する。
func (h *authHandler) respondWithSession(w http.ResponseWriter, r *http.Request, user domain.User, status int) {
	token, hash, err := h.newToken()
	if err != nil {
		h.internalError(w, r, "リフレッシュトークンの生成に失敗した", err)
		return
	}
	expiresAt := h.now().Add(h.opts.RefreshTTL)

	if err := h.repo.CreateRefreshToken(r.Context(), user.ID, hash, expiresAt); err != nil {
		h.internalError(w, r, "リフレッシュトークンの保存に失敗した", err)
		return
	}

	h.setRefreshCookie(w, r, token, expiresAt)
	h.writeAuthResponse(w, r, user, status)
}

func (h *authHandler) writeAuthResponse(w http.ResponseWriter, r *http.Request, user domain.User, status int) {
	now := h.now()
	accessToken, expiresAt, err := h.issuer.Issue(user.ID, now)
	if err != nil {
		h.internalError(w, r, "アクセストークンの発行に失敗した", err)
		return
	}

	var body gen.AuthResponse
	body.Data.AccessToken = accessToken
	body.Data.ExpiresIn = int(expiresAt.Sub(now).Seconds())
	body.Data.User = toAPIUser(user)

	writeJSON(w, r, status, body.Data)
}

func (h *authHandler) setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	// **画像配信の Cookie もここで置き直す。** 画面は15分ごとに
	// アクセストークンを取り直すため、使い続けている限り切れない。
	h.cdn.issue(r.Context(), w, h.now())

	// **残り秒数は1度だけ数える。** 2回数えると、その間に進んだ分だけ
	// 印とトークンの寿命がずれる（実際に1秒ずれた）。
	maxAge := int(time.Until(expiresAt).Seconds())

	h.setSessionHintCookie(w, expiresAt, maxAge)
	http.SetCookie(w, &http.Cookie{
		Name:  refreshCookieName,
		Value: token,
		Path:  refreshCookiePath,
		// HttpOnly: JavaScript から読めなくする。XSS でトークンを持ち出せない。
		HttpOnly: true,
		// Secure: HTTPS でのみ送る。ローカルは HTTP のため false になる。
		Secure: h.opts.CookieSecure,
		// SameSite=Strict: 他サイトからの遷移では送られない。CSRF の主防御。
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
		MaxAge:   maxAge,
	})
}

// setSessionHintCookie はセッションの有無を示す印を置く。
//
// **値に意味を持たせない。** 利用者 ID や期限を入れると、
// 「読めても害が無い」という前提が崩れる。
func (h *authHandler) setSessionHintCookie(w http.ResponseWriter, expiresAt time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:  sessionHintCookieName,
		Value: "1",
		Path:  "/",
		// **HttpOnly にしない。** 画面が読めることが目的である。
		HttpOnly: false,
		Secure:   h.opts.CookieSecure,
		// 印だけなので Lax でよいが、本体と揃えて驚きを減らす。
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
		MaxAge:   maxAge,
	})
}

func (h *authHandler) clearSessionHintCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionHintCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   h.opts.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (h *authHandler) clearRefreshCookie(w http.ResponseWriter) {
	h.clearSessionHintCookie(w)
	h.cdn.clear(w)
	// 削除するときも Path と属性を発行時と揃える。
	// 揃っていないとブラウザが別の Cookie とみなし、元の Cookie が残る。
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   h.opts.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// clientIP はレート制限の鍵に使う発信元を返す。
//
// **左端は採らない。** 詳細は AuthOptions.TrustedProxyHops のコメントを参照。
func (h *authHandler) clientIP(r *http.Request) string {
	if h.opts.TrustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip := forwardedHop(xff, h.opts.TrustedProxyHops); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// forwardedHop は X-Forwarded-For の末尾から hops 個目を返す。
//
// 想定より段数が少ない場合は末尾へ倒す。**左へ寄せるとクライアントが書いた値を掴む。**
// 末尾はどの構成でも直前のプロキシが自分で書いた値であり、クライアントには操作できない。
func forwardedHop(xff string, hops int) string {
	if hops < 0 {
		hops = 0
	}
	parts := strings.Split(xff, ",")
	i := len(parts) - 1 - hops
	if i < 0 {
		i = len(parts) - 1
	}
	return strings.TrimSpace(parts[i])
}

func (h *authHandler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	respondInternalError(w, r, h.logger, msg, err)
}

func toAPIUser(u domain.User) gen.User {
	return gen.User{
		Id:          int64(u.ID),
		Handle:      u.Handle,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
	}
}

// decodeJSON は本文を読み取る。失敗時は 400 を返して false を返す。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	// 本文の大きさに上限を設ける。無いと、巨大な本文を送られるだけで
	// メモリを使い切らせられる。
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB

	dec := json.NewDecoder(r.Body)
	// 仕様に無いフィールドを拒否する。書き間違えたフィールド名が
	// 黙って無視され「設定したのに効かない」状態になるのを防ぐ。
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "リクエストの形式が正しくありません")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 入力の検証
//
// 生成した型（gen.SignupRequest）に検証タグを付けられないため、
// 宣言的な検証ライブラリではなく関数として書く。
// 仕様（docs/openapi.yaml）に書いた制約と対応させること。
// ---------------------------------------------------------------------------

var (
	handlePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	// メールアドレスの完全な検証は行わない。RFC に厳密に従う正規表現は
	// 正しく書くのが難しく、正当なアドレスを弾く事故のほうが起きやすい。
	// 「@ を挟んで前後に文字があり、空白を含まない」程度に留め、
	// 実在性の確認はそもそも行わない方針である（メール送信が対象外のため）。
	emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

func validateSignup(email, handle, displayName, password string) string {
	if !emailPattern.MatchString(email) || len(email) > 255 {
		return "メールアドレスの形式が正しくありません"
	}
	if n := len(handle); n < 3 || n > 30 || !handlePattern.MatchString(handle) {
		return "ハンドルは英数字とアンダースコアで3〜30文字にしてください"
	}
	// 表示名は文字数で数える。日本語の名前がバイト数で弾かれると
	// 「16文字で長すぎると言われる」ことになる。
	if n := utf8.RuneCountInString(displayName); n < 1 || n > 50 {
		return "表示名は1〜50文字にしてください"
	}
	// パスワードはバイト数で数える。bcrypt の制限がバイト単位のため。
	if err := auth.ValidatePasswordLength(password); err != nil {
		return "パスワードは8〜72バイトにしてください（日本語は1文字3バイト程度です）"
	}
	return ""
}
