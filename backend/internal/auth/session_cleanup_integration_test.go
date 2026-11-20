//go:build integration

package auth

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCleanupExpiredSessionsPreservesActiveSessions(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var userID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ('session-cleanup@example.test', 'not-used', 'Cleanup Test')
		RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sessions := []Session{
		{UserID: userID, TokenHash: bytes.Repeat([]byte{1}, 32), CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour), IdleExpiresAt: now.Add(time.Hour)},
		{UserID: userID, TokenHash: bytes.Repeat([]byte{2}, 32), CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(-time.Minute)},
		{UserID: userID, TokenHash: bytes.Repeat([]byte{3}, 32), CreatedAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(15 * time.Minute)},
	}
	store := NewStore(tx)
	for _, session := range sessions {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	for run := 1; run <= 2; run++ {
		deleted, err := store.CleanupExpiredSessions(ctx, now, 1)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 1 {
			t.Fatalf("cleanup run %d deleted %d sessions, want 1", run, deleted)
		}
	}
	deleted, err := store.CleanupExpiredSessions(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("final cleanup deleted %d sessions", deleted)
	}
	var activeCount int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1 AND token_hash = $2", userID, sessions[2].TokenHash).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Fatal("active session was removed")
	}
}
