package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/auth"
	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/search"
	"github.com/doryu0-04092/tabi-log/backend/internal/storage"
	"github.com/doryu0-04092/tabi-log/backend/internal/store"

	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret-only-used-in-unit-tests"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testTokens(t *testing.T) *auth.JWTService {
	t.Helper()
	key := auth.SigningKey{ID: "v1", Method: jwt.SigningMethodHS256, Secret: []byte(testJWTSecret)}
	s, err := auth.NewJWTService(key, []auth.SigningKey{key}, 15*time.Minute)
	if err != nil {
		t.Fatalf("トークンサービスの作成に失敗した: %v", err)
	}
	return s
}

// stubPinger は疎通確認の結果を固定で返す。
type stubPinger struct{ err error }

func (s stubPinger) PingContext(context.Context) error { return s.err }

// stubPrefectureLister は固定の結果を返す。
type stubPrefectureLister struct {
	items []domain.Prefecture
	err   error
}

func (s stubPrefectureLister) List(context.Context) ([]domain.Prefecture, error) {
	return s.items, s.err
}

// testDeps はテスト用の依存一式を組み立てる。
type testDeps struct {
	pingErr       error
	prefectures   PrefectureLister
	auth          AuthRepository
	posts         PostRepository
	storage       ObjectStorage
	reactions     ReactionRepository
	follows       FollowRepository
	search        SearchRepository
	notifications NotificationRepository
	account       AccountRepository
	tokens        *auth.JWTService

	// 書き込みの上限。0 なら緩い既定値を使う。
	// **上限そのものを見るテストだけがここを絞る。**
	postCreateLimit    int
	commentCreateLimit int
	uploadLimit        int
	isProduction       bool

	// loginAttemptLimit は認証の上限。0 なら緩い既定値を使う。
	loginAttemptLimit int

	// cdn は画像配信の署名付き Cookie を発行する係。
	// **nil なら Cookie を置かない**（ローカルと同じ状態）。
	cdn *storage.CDNSigner
}

func newRouter(t *testing.T, d testDeps) http.Handler {
	t.Helper()

	if d.tokens == nil {
		d.tokens = testTokens(t)
	}
	if d.prefectures == nil {
		d.prefectures = stubPrefectureLister{items: []domain.Prefecture{}}
	}
	if d.auth == nil {
		d.auth = &stubAuthRepo{}
	}
	if d.posts == nil {
		d.posts = &stubPostRepo{}
	}
	if d.storage == nil {
		d.storage = &stubStorage{}
	}
	if d.reactions == nil {
		d.reactions = &stubReactionRepo{}
	}
	if d.follows == nil {
		d.follows = &stubFollowRepo{}
	}
	if d.search == nil {
		d.search = &stubSearchRepo{}
	}
	if d.notifications == nil {
		d.notifications = &stubNotificationRepo{}
	}
	if d.account == nil {
		d.account = &stubAccountRepo{}
	}
	if d.postCreateLimit == 0 {
		d.postCreateLimit = 1000
	}
	if d.commentCreateLimit == 0 {
		d.commentCreateLimit = 1000
	}
	if d.uploadLimit == 0 {
		d.uploadLimit = 1000
	}
	if d.loginAttemptLimit == 0 {
		d.loginAttemptLimit = 1000
	}

	deps := Deps{
		DB:            stubPinger{err: d.pingErr},
		Prefectures:   d.prefectures,
		Auth:          d.auth,
		Posts:         d.posts,
		Storage:       d.storage,
		Reactions:     d.reactions,
		Follows:       d.follows,
		Search:        d.search,
		Notifications: d.notifications,
		Account:       d.account,
		TokenIssuer:   d.tokens,
		TokenVerifier: d.tokens,
		AuthOptions: AuthOptions{
			RefreshTTL:   7 * 24 * time.Hour,
			RefreshGrace: 10 * time.Second,
			CookieSecure: false,
		},
		LoginAttemptLimit:  d.loginAttemptLimit,
		LoginAttemptWindow: 5 * time.Minute,
		// **書き込みの上限はテストでは緩めておく。** 上限そのものを
		// 見るテストは、上限を絞った専用の構成で確かめる（writelimit_test.go）。
		// ここを既定のままにすると、無関係なテストが 429 で落ちる。
		CDNCookies:         d.cdn,
		CDNCookieTTL:       24 * time.Hour,
		PostCreateLimit:    d.postCreateLimit,
		CommentCreateLimit: d.commentCreateLimit,
		UploadLimit:        d.uploadLimit,
		IsProduction:       d.isProduction,
		WriteLimitWindow:   time.Hour,
		Logger:             discardLogger(),
	}

	return NewRouter(deps)
}

// stubAuthRepo は AuthRepository の差し替え可能な実装。
//
// 各関数フィールドを設定しなければ、素直な既定動作をする。
type stubAuthRepo struct {
	users        map[string]domain.Credentials // email -> credentials
	byID         map[uint64]domain.User
	createErr    error
	createdUser  domain.User
	rotateUserID uint64
	rotateErr    error
	revokedHash  string
	savedTokens  []string
	findByIDErr  error

	// rehashed は UpdatePasswordHash に渡された値。付け直しの検証に使う。
	rehashed  map[uint64]string
	rehashErr error
}

func (s *stubAuthRepo) UpdatePasswordHash(_ context.Context, userID uint64, hash string) error {
	if s.rehashErr != nil {
		return s.rehashErr
	}
	if s.rehashed == nil {
		s.rehashed = map[uint64]string{}
	}
	s.rehashed[userID] = hash
	return nil
}

func (s *stubAuthRepo) CreateUser(_ context.Context, handle, email, passwordHash, displayName string) (domain.User, error) {
	if s.createErr != nil {
		return domain.User{}, s.createErr
	}
	u := domain.User{ID: 1, Handle: handle, Email: email, DisplayName: displayName}
	if s.createdUser.ID != 0 {
		u = s.createdUser
	}
	if s.byID == nil {
		s.byID = map[uint64]domain.User{}
	}
	s.byID[u.ID] = u
	_ = passwordHash
	return u, nil
}

// **大文字小文字を区別しない。** users.email の照合順序は
// utf8mb4_0900_ai_ci で、実際の DB は区別しない。区別する偽物を使うと、
// 本番では通る経路がテストでだけ落ちる。
func (s *stubAuthRepo) FindCredentialsByEmail(_ context.Context, email string) (domain.Credentials, error) {
	c, ok := s.users[strings.ToLower(email)]
	if !ok {
		return domain.Credentials{}, errUserNotFoundForTest
	}
	return c, nil
}

func (s *stubAuthRepo) FindUserByID(_ context.Context, id uint64) (domain.User, error) {
	if s.findByIDErr != nil {
		return domain.User{}, s.findByIDErr
	}
	u, ok := s.byID[id]
	if !ok {
		return domain.User{}, errUserNotFoundForTest
	}
	return u, nil
}

func (s *stubAuthRepo) CreateRefreshToken(_ context.Context, _ uint64, hash string, _ time.Time) error {
	s.savedTokens = append(s.savedTokens, hash)
	return nil
}

func (s *stubAuthRepo) RotateRefreshToken(_ context.Context, _, _ string, _, _ time.Time, _ time.Duration) (uint64, error) {
	if s.rotateErr != nil {
		return 0, s.rotateErr
	}
	return s.rotateUserID, nil
}

func (s *stubAuthRepo) RevokeRefreshTokenByHash(_ context.Context, hash string, _ time.Time) error {
	s.revokedHash = hash
	return nil
}

// errUserNotFoundForTest は store.ErrUserNotFound と同じ扱いにするため、
// テスト側でも同一のエラー値を使う。
var errUserNotFoundForTest = store.ErrUserNotFound

// リクエストの組み立て補助。

func postJSON(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withCSRF(r *http.Request) *http.Request {
	r.Header.Set(csrfHeaderName, csrfHeaderValue)
	return r
}

func withBearer(r *http.Request, token string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func mustIssue(t *testing.T, s *auth.JWTService, userID uint64) string {
	t.Helper()
	token, _, err := s.Issue(userID, time.Now())
	if err != nil {
		t.Fatalf("トークンの発行に失敗した: %v", err)
	}
	return token
}

func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// stubPostRepo は PostRepository のうち、反応のテストで使う部分だけを持つ。
//
// 使わないメソッドは「呼ばれたら失敗」にしていない。呼ばれても
// 素直な零値を返すほうが、テストの意図（何を確かめているか）が読みやすい。
type stubPostRepo struct {
	originalKeys    []string
	originalKeysErr error
	owner           uint64
	ownerErr        error
	posts           []domain.Post
	nextCursor      uint64
	listErr         error

	// フォロー中フィードは閲覧者ごとに違う。誰の分を引いたかを記録する。
	lastViewerID uint64
	// 検索が決めた並びが保たれているかを見るために記録する。
	lastIDs []uint64

	// 旅行履歴はカーソルの形が違う（訪問日と ID の組）。
	lastTravelCursor store.TravelCursor
	nextTravelCursor store.TravelCursor

	// created は保存まで届いた投稿。上限の確認で「弾いた分は
	// 保存していない」ことを見るために記録する。
	created []store.CreatePostInput
}

func (s *stubPostRepo) CreatePendingMedia(context.Context, uint64, string) (uint64, error) {
	return 0, nil
}
func (s *stubPostRepo) CreatePost(_ context.Context, in store.CreatePostInput) (uint64, error) {
	s.created = append(s.created, in)
	return uint64(len(s.created)), nil
}
func (s *stubPostRepo) UpdatePost(context.Context, store.UpdatePostInput) error { return nil }
func (s *stubPostRepo) DeletePost(context.Context, uint64, uint64) ([]string, error) {
	return nil, nil
}
func (s *stubPostRepo) PostOwner(context.Context, uint64) (uint64, error) {
	return s.owner, s.ownerErr
}
func (s *stubPostRepo) FindMediaByID(context.Context, uint64) (store.MediaRecord, error) {
	return store.MediaRecord{}, nil
}

// originalKeys は ListOriginalKeys が返すキー。既定で1件返す。
// **既定を空にしない。** 空だと保持印の呼び出しが起きず、
// 「呼ばれること」を見るテストが黙って通ってしまう。
func (s *stubPostRepo) ListOriginalKeys(context.Context, uint64) ([]string, error) {
	if s.originalKeysErr != nil {
		return nil, s.originalKeysErr
	}
	if s.originalKeys == nil {
		return []string{"originals/1/a.jpg"}, nil
	}
	return s.originalKeys, nil
}
func (s *stubPostRepo) GetPost(context.Context, uint64, storage.URLSigner, time.Duration) (domain.Post, error) {
	return domain.Post{}, nil
}
func (s *stubPostRepo) ListFeed(context.Context, uint64, int, storage.URLSigner, time.Duration) ([]domain.Post, uint64, error) {
	return s.posts, s.nextCursor, s.listErr
}
func (s *stubPostRepo) ListUserPosts(context.Context, uint64, uint64, int, storage.URLSigner, time.Duration) ([]domain.Post, uint64, error) {
	return s.posts, s.nextCursor, s.listErr
}
func (s *stubPostRepo) ListFollowingFeed(_ context.Context, viewerID, _ uint64, _ int, _ storage.URLSigner, _ time.Duration) ([]domain.Post, uint64, error) {
	s.lastViewerID = viewerID
	return s.posts, s.nextCursor, s.listErr
}

// 検索は ID の並びだけを決め、本体はここで組み立てられる。
// 並びが保たれることを確かめられるよう、渡された順に返す。
func (s *stubPostRepo) ListUserTravels(_ context.Context, _ uint64, cursor store.TravelCursor, _ int, _ storage.URLSigner, _ time.Duration) ([]domain.Post, store.TravelCursor, error) {
	s.lastTravelCursor = cursor
	return s.posts, s.nextTravelCursor, s.listErr
}

func (s *stubPostRepo) ListPostsByIDs(_ context.Context, ids []uint64, _ storage.URLSigner, _ time.Duration) ([]domain.Post, error) {
	s.lastIDs = ids
	byID := make(map[uint64]domain.Post, len(s.posts))
	for _, p := range s.posts {
		byID[p.ID] = p
	}
	out := make([]domain.Post, 0, len(ids))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, p)
		}
	}
	return out, s.listErr
}

// stubReactionRepo は呼び出しを記録し、返す値をテストごとに差し替えられる。
type stubReactionRepo struct {
	likeCalls   [][2]uint64 // {userID, postID}
	unlikeCalls [][2]uint64
	likeErr     error

	created     []string
	createID    uint64
	createErr   error
	comments    []domain.Comment
	nextCursor  uint64
	listErr     error
	perm        store.CommentPermission
	permErr     error
	deleted     []uint64
	deleteErr   error
	lastLimit   int
	lastCursor  uint64
	lastPostIDs []uint64
}

func (s *stubReactionRepo) Like(_ context.Context, userID, postID uint64) error {
	s.likeCalls = append(s.likeCalls, [2]uint64{userID, postID})
	return s.likeErr
}

func (s *stubReactionRepo) Unlike(_ context.Context, userID, postID uint64) error {
	s.unlikeCalls = append(s.unlikeCalls, [2]uint64{userID, postID})
	return s.likeErr
}

func (s *stubReactionRepo) LikedPostIDs(_ context.Context, _ uint64, postIDs []uint64) (map[uint64]bool, error) {
	s.lastPostIDs = postIDs
	return map[uint64]bool{}, nil
}

func (s *stubReactionRepo) CreateComment(_ context.Context, _, _ uint64, body string) (uint64, error) {
	s.created = append(s.created, body)
	return s.createID, s.createErr
}

func (s *stubReactionRepo) DeleteComment(_ context.Context, commentID, _ uint64) error {
	s.deleted = append(s.deleted, commentID)
	return s.deleteErr
}

func (s *stubReactionRepo) FindCommentPermission(context.Context, uint64) (store.CommentPermission, error) {
	return s.perm, s.permErr
}

func (s *stubReactionRepo) ListComments(_ context.Context, _, cursorID uint64, limit int) ([]domain.Comment, uint64, error) {
	s.lastCursor = cursorID
	s.lastLimit = limit
	return s.comments, s.nextCursor, s.listErr
}

func (s *stubReactionRepo) GetComment(_ context.Context, commentID uint64) (domain.Comment, error) {
	for _, c := range s.comments {
		if c.ID == commentID {
			return c, nil
		}
	}
	return domain.Comment{}, store.ErrCommentNotFound
}

// stubFollowRepo は呼び出しを記録し、返す値をテストごとに差し替えられる。
type stubFollowRepo struct {
	users map[string]domain.User // handle -> user

	followCalls   [][2]uint64 // {followerID, followeeID}
	unfollowCalls [][2]uint64
	followErr     error

	profile    store.UserProfile
	profileErr error

	list       []domain.User
	nextCursor uint64
	listErr    error
	followed   map[uint64]bool

	prefectures    []domain.PrefectureCount
	prefecturesErr error

	lastCursor uint64
	lastLimit  int
}

func (s *stubFollowRepo) FindUserByHandle(_ context.Context, handle string) (domain.User, error) {
	u, ok := s.users[handle]
	if !ok {
		return domain.User{}, store.ErrUserNotFoundByHandle
	}
	return u, nil
}

func (s *stubFollowRepo) Profile(_ context.Context, handle string, _ uint64) (store.UserProfile, error) {
	if s.profileErr != nil {
		return store.UserProfile{}, s.profileErr
	}
	if _, ok := s.users[handle]; !ok {
		return store.UserProfile{}, store.ErrUserNotFoundByHandle
	}
	return s.profile, nil
}

func (s *stubFollowRepo) Follow(_ context.Context, followerID, followeeID uint64) error {
	s.followCalls = append(s.followCalls, [2]uint64{followerID, followeeID})
	if followerID == followeeID {
		return store.ErrCannotFollowSelf
	}
	return s.followErr
}

func (s *stubFollowRepo) Unfollow(_ context.Context, followerID, followeeID uint64) error {
	s.unfollowCalls = append(s.unfollowCalls, [2]uint64{followerID, followeeID})
	return s.followErr
}

func (s *stubFollowRepo) ListFollowers(_ context.Context, _, cursorID uint64, limit int) ([]domain.User, uint64, error) {
	s.lastCursor, s.lastLimit = cursorID, limit
	return s.list, s.nextCursor, s.listErr
}

func (s *stubFollowRepo) ListFollowing(_ context.Context, _, cursorID uint64, limit int) ([]domain.User, uint64, error) {
	s.lastCursor, s.lastLimit = cursorID, limit
	return s.list, s.nextCursor, s.listErr
}

func (s *stubFollowRepo) PrefectureCounts(context.Context, uint64) ([]domain.PrefectureCount, error) {
	return s.prefectures, s.prefecturesErr
}

func (s *stubFollowRepo) FollowedUserIDs(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	if s.followed == nil {
		return map[uint64]bool{}, nil
	}
	return s.followed, nil
}

// stubSearchRepo は検索の入力を記録し、返す値を差し替えられる。
type stubSearchRepo struct {
	ids        []uint64
	nextCursor search.Cursor
	postsErr   error

	users        []domain.User
	usersCursor  uint64
	usersErr     error
	lastKeyword  string
	lastFilters  search.Filters
	lastCursor   search.Cursor
	lastLimit    int
	lastUserSeen string
}

func (s *stubSearchRepo) SearchPosts(_ context.Context, f search.Filters, cursor search.Cursor, limit int) ([]uint64, search.Cursor, error) {
	s.lastFilters = f
	s.lastCursor = cursor
	s.lastLimit = limit
	return s.ids, s.nextCursor, s.postsErr
}

func (s *stubSearchRepo) SearchUsers(_ context.Context, keyword string, cursorID uint64, limit int) ([]domain.User, uint64, error) {
	s.lastKeyword = keyword
	s.lastUserSeen = keyword
	s.lastLimit = limit
	_ = cursorID
	return s.users, s.usersCursor, s.usersErr
}

// stubNotificationRepo は呼び出しを記録し、返す値を差し替えられる。
type stubNotificationRepo struct {
	items      []domain.Notification
	nextCursor uint64
	listErr    error

	unread    int
	unreadErr error

	markErr      error
	markedIDs    []uint64
	markedAllFor []uint64

	lastCursor uint64
	lastLimit  int
	lastUserID uint64
}

func (s *stubNotificationRepo) List(_ context.Context, userID, cursorID uint64, limit int) ([]domain.Notification, uint64, error) {
	s.lastUserID, s.lastCursor, s.lastLimit = userID, cursorID, limit
	return s.items, s.nextCursor, s.listErr
}

func (s *stubNotificationRepo) UnreadCount(_ context.Context, userID uint64) (int, error) {
	s.lastUserID = userID
	return s.unread, s.unreadErr
}

func (s *stubNotificationRepo) MarkRead(_ context.Context, notificationID, userID uint64, _ time.Time) error {
	s.lastUserID = userID
	if s.markErr != nil {
		return s.markErr
	}
	s.markedIDs = append(s.markedIDs, notificationID)
	return nil
}

func (s *stubNotificationRepo) MarkAllRead(_ context.Context, userID uint64, _ time.Time) error {
	if s.markErr != nil {
		return s.markErr
	}
	s.markedAllFor = append(s.markedAllFor, userID)
	return nil
}

// stubAccountRepo は呼び出しを記録し、返す値を差し替えられる。
type stubAccountRepo struct {
	current    domain.User
	currentErr error

	hash    string
	hashErr error

	updated       []domain.User
	updateErr     error
	changedHashes []string
	changeErr     error
	deletedFor    []uint64
	deleteKeys    []string
	deleteErr     error

	avatarErr    error
	setAvatarIDs []uint64
	clearedFor   []uint64
	avatarKeys   map[uint64]string
}

func (s *stubAccountRepo) Current(context.Context, uint64) (domain.User, error) {
	return s.current, s.currentErr
}

func (s *stubAccountRepo) UpdateProfile(_ context.Context, userID uint64, displayName string, bio *string) (domain.User, error) {
	if s.updateErr != nil {
		return domain.User{}, s.updateErr
	}
	u := domain.User{ID: userID, Handle: s.current.Handle, DisplayName: displayName, Bio: bio}
	s.updated = append(s.updated, u)
	return u, nil
}

func (s *stubAccountRepo) Credentials(context.Context, uint64) (string, error) {
	return s.hash, s.hashErr
}

func (s *stubAccountRepo) ChangePassword(_ context.Context, _ uint64, newHash string, _ time.Time) error {
	if s.changeErr != nil {
		return s.changeErr
	}
	s.changedHashes = append(s.changedHashes, newHash)
	return nil
}

func (s *stubAccountRepo) DeleteAccount(_ context.Context, userID uint64, _ time.Time) ([]string, error) {
	if s.deleteErr != nil {
		return nil, s.deleteErr
	}
	s.deletedFor = append(s.deletedFor, userID)
	return s.deleteKeys, nil
}

// errStorageForTest は保存先の失敗を表す。
var errStorageForTest = errors.New("保存先で失敗した")

// stubStorage は消した鍵を記録する。
//
// 退会で **S3 のオブジェクトを明示的に消しているか** を確かめるために使う。
type stubStorage struct {
	// keptKeys は MarkKept に渡されたキー。呼ばれたことの検証に使う。
	keptKeys    []string
	markKeptErr error
	deleted     []string
	deleteErr   error
}

func (s *stubStorage) DisplayURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.test/" + key, nil
}

func (s *stubStorage) PresignPut(context.Context, string, string, int64, time.Duration) (string, error) {
	return "https://example.test/upload", nil
}

// MarkKept は「投稿に使われた」印を記録する。**呼ばれたことを検証できるようにする。**
// 印が付かないと原本が期限で消えるため、呼び忘れはテストで捕まえたい。
func (s *stubStorage) MarkKept(_ context.Context, keys ...string) error {
	s.keptKeys = append(s.keptKeys, keys...)
	if s.markKeptErr != nil {
		return s.markKeptErr
	}
	return nil
}

func (s *stubStorage) Delete(_ context.Context, keys ...string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, keys...)
	return nil
}

// mustDate は "YYYY-MM-DD" を時刻に変える。
func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("日付を解釈できない: %v", err)
	}
	return d
}

// アバターの操作。stubAccountRepo が AvatarRepository も兼ねる。
func (s *stubAccountRepo) SetAvatar(_ context.Context, _, mediaID uint64) error {
	if s.avatarErr != nil {
		return s.avatarErr
	}
	s.setAvatarIDs = append(s.setAvatarIDs, mediaID)
	return nil
}

func (s *stubAccountRepo) ClearAvatar(_ context.Context, userID uint64) error {
	s.clearedFor = append(s.clearedFor, userID)
	return nil
}

func (s *stubAccountRepo) AvatarKeys(context.Context, []uint64) (map[uint64]string, error) {
	if s.avatarKeys == nil {
		return map[uint64]string{}, nil
	}
	return s.avatarKeys, nil
}
