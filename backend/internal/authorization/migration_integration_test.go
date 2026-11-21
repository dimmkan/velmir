//go:build integration

package authorization

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFieldPermissionMigrationBackfillsUnrestrictedAndIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Path != "/velmir_admin" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1") {
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

	roleName := fmt.Sprintf("migration_backfill_%d", time.Now().UnixNano())
	var roleID int64
	if err := tx.QueryRow(ctx, "INSERT INTO roles (name, description) VALUES ($1, '') RETURNING id", roleName).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"read", "create", "update"} {
		if _, err := tx.Exec(ctx, "INSERT INTO role_permissions (role_id, schema_name, resource_name, permission) VALUES ($1, 'public', 'books', $2)", roleID, action); err != nil {
			t.Fatal(err)
		}
	}
	for _, migrationName := range []string{"00008_field_permissions.sql", "00009_read_field_permissions.sql"} {
		migration, err := os.ReadFile("../../migrations/" + migrationName)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(migration), "-- +goose Down")[0]
		for attempt := 0; attempt < 2; attempt++ {
			if _, err := tx.Exec(ctx, up); err != nil {
				t.Fatalf("apply %s attempt %d: %v", migrationName, attempt+1, err)
			}
		}
	}
	rows, err := tx.Query(ctx, "SELECT action, mode, columns FROM role_field_permissions WHERE role_id = $1 ORDER BY action", roleID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var action, mode string
		var columns []string
		if err := rows.Scan(&action, &mode, &columns); err != nil {
			t.Fatal(err)
		}
		if mode != "unrestricted" || len(columns) != 0 {
			t.Errorf("backfilled %s permission = %s %#v", action, mode, columns)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("backfilled field rows = %d, want 3", count)
	}
	var grants int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM role_permissions WHERE role_id = $1", roleID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 3 {
		t.Fatalf("existing resource grants changed: %d", grants)
	}
}
