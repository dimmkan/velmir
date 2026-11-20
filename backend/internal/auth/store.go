package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("auth record not found")

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	DisplayName  string
	IsActive     bool
}

type Session struct {
	UserID        int64
	TokenHash     []byte
	CreatedAt     time.Time
	ExpiresAt     time.Time
	IdleExpiresAt time.Time
}

type DBTX interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Store interface {
	FindUserByEmail(context.Context, string) (User, error)
	CreateSession(context.Context, Session) error
	TouchSession(context.Context, []byte, time.Time, time.Time) (User, error)
	DeleteSession(context.Context, []byte) error
}

type PostgresStore struct {
	db DBTX
}

func NewStore(db DBTX) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) FindUserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.db.QueryRow(ctx, `
		SELECT id, email, password_hash, display_name, is_active
		FROM users
		WHERE email = $1`, email).
		Scan(&user.ID, &user.Email, &user.PasswordHash, &user.DisplayName, &user.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

func (s *PostgresStore) CreateSession(ctx context.Context, session Session) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, idle_expires_at, created_at, updated_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $5, $5)`,
		session.TokenHash, session.UserID, session.ExpiresAt, session.IdleExpiresAt, session.CreatedAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *PostgresStore) TouchSession(ctx context.Context, tokenHash []byte, now, idleExpiresAt time.Time) (User, error) {
	var user User
	err := s.db.QueryRow(ctx, `
		UPDATE sessions AS s
		SET last_seen_at = $2,
		    idle_expires_at = LEAST(s.expires_at, $3),
		    updated_at = $2
		FROM users AS u
		WHERE s.token_hash = $1
		  AND u.id = s.user_id
		  AND u.is_active
		  AND s.expires_at > $2
		  AND s.idle_expires_at > $2
		RETURNING u.id, u.email, u.password_hash, u.display_name, u.is_active`, tokenHash, now, idleExpiresAt).
		Scan(&user.ID, &user.Email, &user.PasswordHash, &user.DisplayName, &user.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("authenticate session: %w", err)
	}
	return user, nil
}

func (s *PostgresStore) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *PostgresStore) CleanupExpiredSessions(ctx context.Context, now time.Time, batchSize int32) (int64, error) {
	result, err := s.db.Exec(ctx, `
		DELETE FROM sessions
		WHERE token_hash IN (
			SELECT token_hash
			FROM sessions
			WHERE expires_at <= $1 OR idle_expires_at <= $1
			ORDER BY LEAST(expires_at, idle_expires_at)
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)`, now, batchSize)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired sessions: %w", err)
	}
	return result.RowsAffected(), nil
}
