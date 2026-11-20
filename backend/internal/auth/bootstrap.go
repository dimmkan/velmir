package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"velmir/backend/internal/config"
)

var ErrBootstrapCredentialsRequired = errors.New("admin database has no users; set VELMIR_BOOTSTRAP_ADMIN_LOGIN and VELMIR_BOOTSTRAP_ADMIN_PASSWORD")

type BootstrapOptions struct {
	Login       string
	Password    string
	DisplayName string
	AppEnv      string
	DemoMode    bool
}

func Bootstrap(ctx context.Context, db *pgxpool.Pool, options BootstrapOptions) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return false, fmt.Errorf("lock users for bootstrap: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		return false, fmt.Errorf("count users for bootstrap: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	usingDemoDefaults := options.DemoMode && options.AppEnv == config.Development && options.Login == "" && options.Password == ""
	if usingDemoDefaults {
		options.Login = "admin"
		options.Password = "admin"
	}
	options.Login = NormalizeEmail(options.Login)
	if options.Login == "" || options.Password == "" {
		return false, ErrBootstrapCredentialsRequired
	}
	if len(options.Login) > 254 || strings.ContainsAny(options.Login, " \t\r\n") {
		return false, errors.New("VELMIR_BOOTSTRAP_ADMIN_LOGIN must be a non-empty login without whitespace and at most 254 characters")
	}
	if !usingDemoDefaults {
		if err := validateNewUserPassword(options.Password); err != nil {
			return false, fmt.Errorf("VELMIR_BOOTSTRAP_ADMIN_PASSWORD: %w", err)
		}
	}
	if options.AppEnv == config.Production && trivialBootstrapCredentials(options.Login, options.Password) {
		return false, errors.New("trivial bootstrap credentials are forbidden when APP_ENV=production; use a non-trivial login and a long random password")
	}
	options.DisplayName = strings.TrimSpace(options.DisplayName)
	if options.DisplayName == "" {
		options.DisplayName = "Administrator"
	}
	if len(options.DisplayName) > 200 {
		return false, errors.New("VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME must not exceed 200 characters")
	}
	passwordHash, err := HashPassword(options.Password)
	if err != nil {
		return false, err
	}
	var userID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		RETURNING id`, options.Login, passwordHash, options.DisplayName, time.Now().UTC()).Scan(&userID); err != nil {
		return false, fmt.Errorf("create bootstrap administrator: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE name = 'admin'
		ON CONFLICT DO NOTHING`, userID); err != nil {
		return false, fmt.Errorf("assign bootstrap administrator role: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit bootstrap administrator: %w", err)
	}
	return true, nil
}

func trivialBootstrapCredentials(login, password string) bool {
	normalizedLogin := NormalizeEmail(login)
	normalizedPassword := strings.ToLower(strings.TrimSpace(password))
	if normalizedLogin == "admin" && normalizedPassword == "admin" {
		return true
	}
	trivialPasswords := map[string]bool{
		"admin": true, "password": true, "password123": true, "password1234": true,
		"admin123": true, "admin1234": true, "adminadmin": true, "adminadminadmin": true,
		"changeme": true, "changeme123": true, "velmir": true, "velmir123": true,
		"123456": true, "12345678": true, "123456789012": true, "qwerty123456": true,
	}
	return trivialPasswords[normalizedPassword]
}
