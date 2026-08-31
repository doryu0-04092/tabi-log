package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/doryu0-04092/tabi-log/backend/internal/domain"
	"github.com/doryu0-04092/tabi-log/backend/internal/store/dbgen"

	"github.com/go-sql-driver/mysql"
)

// 認証に関する store のエラー。呼び出し側は errors.Is で判別する。
var (
	ErrEmailTaken   = errors.New("そのメールアドレスは既に使われている")
	ErrHandleTaken  = errors.New("そのハンドルは既に使われている")
	ErrUserNotFound = errors.New("利用者が見つからない")

	ErrRefreshTokenNotFound = errors.New("リフレッシュトークンが存在しない")
	ErrRefreshTokenExpired  = errors.New("リフレッシュトークンの期限が切れている")

	// ErrRefreshTokenReused は失効済みトークンの再提示を検知したことを表す。
	// このエラーが返る時点で、そのユーザーの全トークンは失効済みである。
	ErrRefreshTokenReused = errors.New("失効済みのリフレッシュトークンが再提示された")
)

// mysqlDuplicateEntry は一意制約違反のエラー番号。
const mysqlDuplicateEntry = 1062

// AuthStore は利用者とリフレッシュトークンを扱う。
//
// *sql.DB を保持しているのは、リフレッシュのローテーションが
// 複数の文を1つのトランザクションで実行する必要があるためである。
type AuthStore struct {
	db *sql.DB
	q  *dbgen.Queries
}

func NewAuthStore(db *sql.DB) *AuthStore {
	return &AuthStore{db: db, q: dbgen.New(db)}
}

// CreateUser は利用者を作る。
//
// 事前に「そのメールアドレスは使われているか」を SELECT で確認しない。
// 確認と INSERT の間に別のリクエストが割り込むと、確認をすり抜けて
// 一意制約違反になる（time-of-check to time-of-use）。
// **一意性の判定はデータベースに任せ、返ってきたエラーを解釈する。**
func (s *AuthStore) CreateUser(ctx context.Context, handle, email, passwordHash, displayName string) (domain.User, error) {
	res, err := s.q.CreateUser(ctx, dbgen.CreateUserParams{
		Handle:       handle,
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
	})
	if err != nil {
		if dup := duplicateKeyName(err); dup != "" {
			switch {
			case strings.Contains(dup, "email"):
				return domain.User{}, ErrEmailTaken
			case strings.Contains(dup, "handle"):
				return domain.User{}, ErrHandleTaken
			}
		}
		return domain.User{}, fmt.Errorf("利用者の作成に失敗した: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.User{}, fmt.Errorf("作成した利用者のIDを取得できない: %w", err)
	}

	return domain.User{
		ID:          uint64(id),
		Handle:      handle,
		Email:       email,
		DisplayName: displayName,
	}, nil
}

// FindCredentialsByEmail はログインの照合に使う情報を返す。
func (s *AuthStore) FindCredentialsByEmail(ctx context.Context, email string) (domain.Credentials, error) {
	row, err := s.q.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Credentials{}, ErrUserNotFound
	}
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("利用者の取得に失敗した: %w", err)
	}

	return domain.Credentials{
		User: domain.User{
			ID:          row.ID,
			Handle:      row.Handle,
			Email:       row.Email,
			DisplayName: row.DisplayName,
			Bio:         nullStringToPtr(row.Bio),
		},
		PasswordHash: row.PasswordHash,
	}, nil
}

// FindUserByID は利用者を返す。
func (s *AuthStore) FindUserByID(ctx context.Context, id uint64) (domain.User, error) {
	row, err := s.q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("利用者の取得に失敗した: %w", err)
	}
	return domain.User{
		ID:          row.ID,
		Handle:      row.Handle,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Bio:         nullStringToPtr(row.Bio),
	}, nil
}

// CreateRefreshToken は新しいリフレッシュトークンを保存する。
// hash は平文ではなく SHA-256 のハッシュであること。
func (s *AuthStore) CreateRefreshToken(ctx context.Context, userID uint64, hash string, expiresAt time.Time) error {
	_, err := s.q.CreateRefreshToken(ctx, dbgen.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return fmt.Errorf("リフレッシュトークンの保存に失敗した: %w", err)
	}
	return nil
}

// RotateRefreshToken は提示されたトークンを検証し、新しいトークンへ置き換える。
//
// **全体を1つのトランザクションで行う。** 「旧を失効させる」と「新を発行する」が
// 分かれていると、その間にプロセスが停止した場合に
// 「どちらも無効（＝強制ログアウト）」か「両方有効（＝失効し損ね）」が生じる。
//
// grace は正規のローテーション直後に旧トークンが提示された場合の猶予時間である。
// タブを複数開いた利用者は同じトークンで同時にリフレッシュを試みるため、
// これが無いと後発のリクエストが盗用と判定され、正常な利用者が全ログアウトされる。
func (s *AuthStore) RotateRefreshToken(
	ctx context.Context,
	presentedHash, newHash string,
	newExpiresAt, now time.Time,
	grace time.Duration,
) (userID uint64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("トランザクションを開始できない: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := s.q.WithTx(tx)

	// FOR UPDATE で行を固定する。同時に届いた2つのリフレッシュを直列化し、
	// 両方が「まだ失効していない」と読むことを防ぐ。
	row, err := q.GetRefreshTokenByHashForUpdate(ctx, presentedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrRefreshTokenNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("リフレッシュトークンの取得に失敗した: %w", err)
	}

	if !row.ExpiresAt.After(now) {
		return 0, ErrRefreshTokenExpired
	}

	alreadyRevoked := row.RevokedAt.Valid
	if alreadyRevoked {
		withinGrace := row.ReplacedBy.Valid && now.Sub(row.RevokedAt.Time) <= grace
		if !withinGrace {
			// 盗用の兆候とみなす。**そのユーザーの全トークンを失効させる。**
			// 攻撃者が盗んだトークンでローテーションを済ませている場合、
			// 正規の利用者の手元にあるトークンが「失効済みの再提示」として現れる。
			// どちらが正規かを判定できないため、両方を無効にして再ログインさせる。
			if err := q.RevokeAllRefreshTokensForUser(ctx, dbgen.RevokeAllRefreshTokensForUserParams{
				RevokedAt: sql.NullTime{Time: now, Valid: true},
				UserID:    row.UserID,
			}); err != nil {
				return 0, fmt.Errorf("全トークンの失効に失敗した: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return 0, fmt.Errorf("全トークンの失効を確定できない: %w", err)
			}
			return 0, ErrRefreshTokenReused
		}
		// 猶予時間内。並行リフレッシュとみなし、新しいトークンを兄弟として発行する。
		// 旧行の replaced_by は最初の後継を指したまま書き換えない。
		// 「いつ・何に置き換えられたか」の記録を壊さないためである。
	}

	res, err := q.CreateRefreshToken(ctx, dbgen.CreateRefreshTokenParams{
		UserID:    row.UserID,
		TokenHash: newHash,
		ExpiresAt: newExpiresAt,
	})
	if err != nil {
		return 0, fmt.Errorf("新しいリフレッシュトークンの保存に失敗した: %w", err)
	}

	if !alreadyRevoked {
		newID, err := res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("新しいトークンのIDを取得できない: %w", err)
		}
		if err := q.RevokeRefreshToken(ctx, dbgen.RevokeRefreshTokenParams{
			RevokedAt:  sql.NullTime{Time: now, Valid: true},
			ReplacedBy: sql.NullInt64{Int64: newID, Valid: true},
			ID:         row.ID,
		}); err != nil {
			return 0, fmt.Errorf("旧トークンの失効に失敗した: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("ローテーションを確定できない: %w", err)
	}
	return row.UserID, nil
}

// RevokeRefreshTokenByHash はログアウトで使う。
// 既に失効していてもエラーにしない（冪等）。
func (s *AuthStore) RevokeRefreshTokenByHash(ctx context.Context, hash string, now time.Time) error {
	if err := s.q.RevokeRefreshTokenByHash(ctx, dbgen.RevokeRefreshTokenByHashParams{
		RevokedAt: sql.NullTime{Time: now, Valid: true},
		TokenHash: hash,
	}); err != nil {
		return fmt.Errorf("リフレッシュトークンの失効に失敗した: %w", err)
	}
	return nil
}

// RevokeAllRefreshTokensForUser はパスワード変更時などに全セッションを断ち切る。
func (s *AuthStore) RevokeAllRefreshTokensForUser(ctx context.Context, userID uint64, now time.Time) error {
	if err := s.q.RevokeAllRefreshTokensForUser(ctx, dbgen.RevokeAllRefreshTokensForUserParams{
		RevokedAt: sql.NullTime{Time: now, Valid: true},
		UserID:    userID,
	}); err != nil {
		return fmt.Errorf("全トークンの失効に失敗した: %w", err)
	}
	return nil
}

// duplicateKeyName は一意制約違反なら違反したキー名を返す。違反でなければ空文字。
//
// MySQL のエラーメッセージから索引名を取り出す。
// どの一意制約に当たったかで、利用者へ返す説明が変わるためである。
func duplicateKeyName(err error) string {
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != mysqlDuplicateEntry {
		return ""
	}
	// 例: Duplicate entry 'a@example.com' for key 'users.uq_users_email'
	const marker = "for key '"
	i := strings.LastIndex(me.Message, marker)
	if i < 0 {
		return ""
	}
	rest := me.Message[i+len(marker):]
	if j := strings.IndexByte(rest, '\''); j >= 0 {
		return rest[:j]
	}
	return rest
}

func nullStringToPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// UpdatePasswordHash はハッシュだけを差し替える。
//
// **パスワードの変更ではない。** 同じパスワードを新しいコストで
// 付け直すために使う。bcrypt はコストをハッシュ自体に記録するため、
// 設定を変えても既存の利用者は古いコストのまま取り残される。
//
// 呼ぶのはログインが成功した直後だけである。**その時点でしか
// 平文を持っていない。**
func (s *AuthStore) UpdatePasswordHash(ctx context.Context, userID uint64, hash string) error {
	if err := s.q.UpdatePassword(ctx, dbgen.UpdatePasswordParams{
		PasswordHash: hash,
		ID:           userID,
	}); err != nil {
		return fmt.Errorf("パスワードのハッシュを更新できない: %w", err)
	}
	return nil
}
