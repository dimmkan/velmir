package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBootstrapCredentialsRequired = errors.New("admin database has no users; set VELMIR_BOOTSTRAP_ADMIN_EMAIL and VELMIR_BOOTSTRAP_ADMIN_PASSWORD")

func Bootstrap(ctx context.Context, db *pgxpool.Pool, email, password string) (bool, error) {
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

	email = NormalizeEmail(email)
	if email == "" || password == "" {
		return false, ErrBootstrapCredentialsRequired
	}
	if len(email) > 254 || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return false, errors.New("VELMIR_BOOTSTRAP_ADMIN_EMAIL must be a valid email address")
	}
	if len(password) < 12 {
		return false, errors.New("VELMIR_BOOTSTRAP_ADMIN_PASSWORD must contain at least 12 characters")
	}
	if len(password) > 1024 {
		return false, errors.New("VELMIR_BOOTSTRAP_ADMIN_PASSWORD must not exceed 1024 characters")
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (email, password_hash, display_name, created_at, updated_at)
		VALUES ($1, $2, 'Administrator', $3, $3)`, email, passwordHash, time.Now().UTC()); err != nil {
		return false, fmt.Errorf("create bootstrap administrator: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit bootstrap administrator: %w", err)
	}
	return true, nil
}
