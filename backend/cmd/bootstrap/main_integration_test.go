//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/config"
)

func TestProductionBootstrapEntrypoint(t *testing.T) {
	serverConfig := localTestServerConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("fresh production database and idempotent rerun", func(t *testing.T) {
		databaseURL := createMigratedDatabase(t, ctx, serverConfig)
		pool := openTestPool(t, ctx, databaseURL)
		assertUserCount(t, ctx, pool, 0)

		login := "Fresh-Admin@Example.Test"
		password := "fresh-production-password-7f3c9a2e"
		logs, err := runBootstrap(t, databaseURL, config.Production, false, login, password)
		if err != nil {
			t.Fatalf("fresh production bootstrap failed: %v", err)
		}
		assertLogSafety(t, logs, databaseURL, password)
		if strings.Contains(logs, "users already exist") || !strings.Contains(logs, "bootstrap administrator created") {
			t.Fatal("fresh production bootstrap did not report administrator creation")
		}

		before := readBootstrapUser(t, ctx, pool)
		if before.Email != "fresh-admin@example.test" || !before.IsActive {
			t.Fatalf("created administrator has unexpected login or status: %#v", before)
		}
		if before.AdminRoles != 1 || before.SystemAdminRoles != 1 {
			t.Fatalf("administrator roles = %d, system admin roles = %d; want one of each", before.AdminRoles, before.SystemAdminRoles)
		}
		if !strings.HasPrefix(before.PasswordHash, "$argon2id$") || strings.Contains(before.PasswordHash, password) || !auth.VerifyPassword(before.PasswordHash, password) {
			t.Fatal("bootstrap password is not stored as a valid Argon2id-only hash")
		}
		service, err := auth.NewService(auth.NewStore(pool), 15*time.Minute, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		loginResult, err := service.Login(ctx, before.Email, password, "127.0.0.1:1234")
		if err != nil {
			t.Fatalf("created administrator cannot authenticate: %v", err)
		}
		if _, err := service.Authenticate(ctx, loginResult.Token); err != nil {
			t.Fatalf("created administrator session cannot be authenticated: %v", err)
		}

		replacementPassword := "replacement-password-must-not-be-used"
		logs, err = runBootstrap(t, databaseURL, config.Production, false, "replacement@example.test", replacementPassword)
		if err != nil {
			t.Fatalf("idempotent bootstrap rerun failed: %v", err)
		}
		assertLogSafety(t, logs, databaseURL, replacementPassword)
		if !strings.Contains(logs, "users already exist") {
			t.Fatal("bootstrap rerun did not report the existing user")
		}
		after := readBootstrapUser(t, ctx, pool)
		if after != before {
			t.Fatal("bootstrap rerun changed the existing administrator or roles")
		}
		assertUserCount(t, ctx, pool, 1)
	})

	t.Run("existing user is preserved without credentials", func(t *testing.T) {
		databaseURL := createMigratedDatabase(t, ctx, serverConfig)
		pool := openTestPool(t, ctx, databaseURL)
		passwordHash, err := auth.HashPassword("existing-user-password-not-changed")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (email, password_hash, display_name)
			VALUES ('existing@example.test', $1, 'Existing User')`, passwordHash); err != nil {
			t.Fatal(err)
		}
		before := readBootstrapUser(t, ctx, pool)
		logs, err := runBootstrap(t, databaseURL, config.Production, false, "", "")
		if err != nil {
			t.Fatalf("bootstrap with an existing user failed: %v", err)
		}
		assertLogSafety(t, logs, databaseURL, "")
		if !strings.Contains(logs, "users already exist") {
			t.Fatal("existing user did not produce the expected skip")
		}
		if after := readBootstrapUser(t, ctx, pool); after != before {
			t.Fatal("bootstrap changed a database with an existing user")
		}
	})

	for _, test := range []struct {
		name      string
		login     string
		password  string
		errorText string
	}{
		{name: "missing login", password: "valid-production-password", errorText: "VELMIR_BOOTSTRAP_ADMIN_LOGIN"},
		{name: "missing password", login: "admin@example.test", errorText: "VELMIR_BOOTSTRAP_ADMIN_PASSWORD"},
		{name: "demo credentials forbidden in production", login: "admin", password: "admin", errorText: "at least 12 characters"},
		{name: "trivial production credentials", login: "admin", password: "password1234", errorText: "trivial bootstrap credentials"},
		{name: "weak production password", login: "admin@example.test", password: "short", errorText: "at least 12 characters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			databaseURL := createMigratedDatabase(t, ctx, serverConfig)
			pool := openTestPool(t, ctx, databaseURL)
			assertUserCount(t, ctx, pool, 0)
			logs, err := runBootstrap(t, databaseURL, config.Production, false, test.login, test.password)
			if err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("invalid credentials error = %v, want text %q; logs = %s", err, test.errorText, logs)
			}
			assertLogSafety(t, logs, databaseURL, test.password)
			if strings.Contains(logs, "users already exist") {
				t.Fatal("invalid production credentials were misreported as an existing-user skip")
			}
			assertUserCount(t, ctx, pool, 0)
		})
	}

	t.Run("demo defaults remain development only", func(t *testing.T) {
		databaseURL := createMigratedDatabase(t, ctx, serverConfig)
		pool := openTestPool(t, ctx, databaseURL)
		logs, err := runBootstrap(t, databaseURL, config.Development, true, "", "")
		if err != nil {
			t.Fatalf("demo bootstrap failed: %v", err)
		}
		assertLogSafety(t, logs, databaseURL, "")
		user := readBootstrapUser(t, ctx, pool)
		if user.Email != "admin" || !auth.VerifyPassword(user.PasswordHash, "admin") {
			t.Fatal("demo defaults were not created")
		}
	})
}

type bootstrapUserSnapshot struct {
	Email            string
	PasswordHash     string
	DisplayName      string
	IsActive         bool
	AdminRoles       int
	SystemAdminRoles int
}

func localTestServerConfig(t *testing.T) *pgx.ConnConfig {
	t.Helper()
	databaseURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_ADMIN_DATABASE_URL is required for the non-skipped bootstrap integration test")
	}
	connectionConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("TEST_ADMIN_DATABASE_URL is invalid")
	}
	host := connectionConfig.Host
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("TEST_ADMIN_DATABASE_URL must point to a loopback PostgreSQL server")
	}
	if connectionConfig.Database != "velmir_admin" {
		t.Fatal("TEST_ADMIN_DATABASE_URL must use the isolated velmir_admin control database")
	}
	return connectionConfig
}

func createMigratedDatabase(t *testing.T, ctx context.Context, serverConfig *pgx.ConnConfig) string {
	t.Helper()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	databaseName := "velmir_bootstrap_" + hex.EncodeToString(random[:])
	server, err := pgx.ConnectConfig(ctx, serverConfig)
	if err != nil {
		t.Fatal("connect to isolated PostgreSQL control database")
	}
	if _, err := server.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{databaseName}.Sanitize()); err != nil {
		server.Close(ctx)
		t.Fatalf("create isolated bootstrap database: %v", err)
	}
	if err := server.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		server, err := pgx.ConnectConfig(cleanupContext, serverConfig)
		if err != nil {
			t.Errorf("connect for isolated database cleanup: %v", err)
			return
		}
		defer server.Close(cleanupContext)
		if _, err := server.Exec(cleanupContext, "DROP DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated bootstrap database: %v", err)
		}
	})

	connectionConfig := serverConfig.Copy()
	connectionConfig.Database = databaseName
	parsedDatabaseURL, err := url.Parse(os.Getenv("TEST_ADMIN_DATABASE_URL"))
	if err != nil {
		t.Fatal("TEST_ADMIN_DATABASE_URL is invalid")
	}
	parsedDatabaseURL.Path = "/" + databaseName
	parsedDatabaseURL.RawPath = ""
	databaseURL := parsedDatabaseURL.String()
	adminDatabase := stdlib.OpenDB(*connectionConfig)
	defer adminDatabase.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(ctx, adminDatabase, "../../migrations"); err != nil {
		t.Fatalf("apply migrations 1-9: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	if err := pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 9 {
		t.Fatalf("schema version = %d, want 9", version)
	}
	return databaseURL
}

func openTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func runBootstrap(t *testing.T, databaseURL, appEnv string, demoMode bool, login, password string) (string, error) {
	t.Helper()
	t.Setenv("ADMIN_DATABASE_URL", databaseURL)
	t.Setenv("ADMIN_DATABASE_URL_FILE", "")
	t.Setenv("TARGET_DATABASE_URL", "")
	t.Setenv("TARGET_DATABASE_URL_FILE", "")
	t.Setenv("TARGET_WRITE_DATABASE_URL", "")
	t.Setenv("VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL", "")
	t.Setenv("APP_ENV", appEnv)
	t.Setenv("VELMIR_DEMO_MODE", fmt.Sprintf("%t", demoMode))
	t.Setenv("SESSION_COOKIE_SECURE", fmt.Sprintf("%t", appEnv == config.Production))
	t.Setenv("FRONTEND_DIST_PATH", "/app/frontend/dist")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN", login)
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD", password)
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL_FILE", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME", "Bootstrap Integration Administrator")
	var output bytes.Buffer
	err := run(slog.New(slog.NewJSONHandler(&output, nil)))
	return output.String(), err
}

func readBootstrapUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) bootstrapUserSnapshot {
	t.Helper()
	assertUserCount(t, ctx, pool, 1)
	var snapshot bootstrapUserSnapshot
	if err := pool.QueryRow(ctx, `
		SELECT u.email, u.password_hash, u.display_name, u.is_active,
		       count(r.id) FILTER (WHERE r.name = 'admin'),
		       count(r.id) FILTER (WHERE r.name = 'admin' AND r.is_system)
		FROM users AS u
		LEFT JOIN user_roles AS ur ON ur.user_id = u.id
		LEFT JOIN roles AS r ON r.id = ur.role_id
		GROUP BY u.id`).Scan(
		&snapshot.Email, &snapshot.PasswordHash, &snapshot.DisplayName, &snapshot.IsActive,
		&snapshot.AdminRoles, &snapshot.SystemAdminRoles,
	); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertUserCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public.users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("public.users count = %d, want %d", count, expected)
	}
}

func assertLogSafety(t *testing.T, logs, databaseURL, password string) {
	t.Helper()
	connectionConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs, databaseURL) || strings.Contains(logs, connectionConfig.Password) {
		t.Fatal("bootstrap logs contain a database secret")
	}
	if password != "" && strings.Contains(logs, password) {
		t.Fatal("bootstrap logs contain the administrator password")
	}
}
