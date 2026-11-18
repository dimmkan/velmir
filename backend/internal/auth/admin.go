package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrRoleNotFound      = errors.New("role not found")
)

type TxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type QueryDBTX interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type UserSummary struct {
	ID          int64
	Email       string
	DisplayName string
	IsActive    bool
	Roles       []string
}

func CreateUser(ctx context.Context, db DBTX, email, displayName, password string) (User, error) {
	email, err := validateNewUserEmail(email)
	if err != nil {
		return User{}, err
	}
	if err := validateNewUserPassword(password); err != nil {
		return User{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = email
	}
	if len(displayName) > 200 {
		return User{}, errors.New("display name must not exceed 200 characters")
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	user := User{Email: email, DisplayName: displayName, IsActive: true}
	err = db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id`, email, passwordHash, displayName).Scan(&user.ID)
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return User{}, ErrUserAlreadyExists
	}
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func AssignRole(ctx context.Context, db TxBeginner, email, roleName string) error {
	email = NormalizeEmail(email)
	roleName = strings.TrimSpace(roleName)
	if email == "" || roleName == "" {
		return errors.New("email and role are required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin role assignment: %w", err)
	}
	defer tx.Rollback(ctx)

	var userID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", email).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return fmt.Errorf("find user for role assignment: %w", err)
	}
	var roleID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE name = $1", roleName).Scan(&roleID); errors.Is(err, pgx.ErrNoRows) {
		return ErrRoleNotFound
	} else if err != nil {
		return fmt.Errorf("find role for assignment: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role assignment: %w", err)
	}
	return nil
}

func ListUsers(ctx context.Context, db QueryDBTX) ([]UserSummary, error) {
	rows, err := db.Query(ctx, `
		SELECT u.id, u.email, u.display_name, u.is_active,
		       COALESCE((
		           SELECT array_agg(r.name ORDER BY r.name)
		           FROM user_roles AS ur
		           JOIN roles AS r ON r.id = ur.role_id
		           WHERE ur.user_id = u.id
		       ), ARRAY[]::text[])
		FROM users AS u
		ORDER BY u.email`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]UserSummary, 0)
	for rows.Next() {
		var user UserSummary
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsActive, &user.Roles); err != nil {
			return nil, fmt.Errorf("read user summary: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func validateNewUserEmail(email string) (string, error) {
	email = NormalizeEmail(email)
	if email == "" || len(email) > 254 || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return "", errors.New("email must be a valid email address")
	}
	return email, nil
}

func validateNewUserPassword(password string) error {
	if len(password) < 12 {
		return errors.New("password must contain at least 12 characters")
	}
	if len(password) > 1024 {
		return errors.New("password must not exceed 1024 characters")
	}
	return nil
}
