//go:build integration

package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLocalAdminDatabaseAuth(t *testing.T) {
	databaseURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || !isLocalAdminDatabase(parsed) {
		t.Fatal("TEST_ADMIN_DATABASE_URL must point to the local velmir_admin database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("local admin database is unavailable")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	password := "integration-test-password-not-for-production"
	passwordHash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var userID int64
	email := "auth-integration@example.test"
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, 'Integration Administrator')
		RETURNING id`, email, passwordHash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(tx), 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(ctx, email, password, "127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(login.Token)
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.Sum256(decoded)
	var storedHash []byte
	if err := tx.QueryRow(ctx, "SELECT token_hash FROM sessions WHERE user_id = $1", userID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if string(storedHash) != string(expectedHash[:]) || strings.Contains(string(storedHash), login.Token) || strings.Contains(passwordHash, password) {
		t.Fatal("admin database contains an unhashed credential")
	}
	if _, err := service.Authenticate(ctx, login.Token); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, login.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, login.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked session error = %v", err)
	}
}

func isLocalAdminDatabase(parsed *url.URL) bool {
	if parsed == nil || parsed.Path != "/velmir_admin" {
		return false
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
