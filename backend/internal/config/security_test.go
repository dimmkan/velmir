package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSecretsFromFiles(t *testing.T) {
	directory := t.TempDir()
	adminFile := writeSecretFile(t, directory, "admin", "postgresql://velmir:file-password@admin-db:5432/velmir_admin?sslmode=disable\n")
	targetFile := writeSecretFile(t, directory, "target", "postgresql://target:file-password@target-db:5432/app?sslmode=require\r\n")
	passwordFile := writeSecretFile(t, directory, "bootstrap", "file-bootstrap-password\n")
	loginFile := writeSecretFile(t, directory, "login", "file-admin@example.test\n")
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "")
	t.Setenv("ADMIN_DATABASE_URL_FILE", adminFile)
	t.Setenv("TARGET_DATABASE_URL", "")
	t.Setenv("TARGET_DATABASE_URL_FILE", targetFile)
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD_FILE", passwordFile)
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN_FILE", loginFile)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminDatabaseURL != "postgresql://velmir:file-password@admin-db:5432/velmir_admin?sslmode=disable" || cfg.TargetDatabaseURL != "postgresql://target:file-password@target-db:5432/app?sslmode=require" {
		t.Fatal("database secrets were not read from file contents")
	}
	if cfg.BootstrapAdminPassword != "file-bootstrap-password" || cfg.BootstrapAdminLogin != "file-admin@example.test" {
		t.Fatal("bootstrap secrets were not read from files")
	}
}

func TestLoadRejectsSecretEnvAndFile(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "postgresql://env-secret@example.test/admin")
	t.Setenv("ADMIN_DATABASE_URL_FILE", filepath.Join(t.TempDir(), "admin"))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") || strings.Contains(err.Error(), "env-secret") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsEmptySecretFileWithoutLeakingContent(t *testing.T) {
	file := writeSecretFile(t, t.TempDir(), "admin", "\r\n")
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "")
	t.Setenv("ADMIN_DATABASE_URL_FILE", file)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.20.0.0/24, 2001:db8::/32")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("trusted proxies = %v", cfg.TrustedProxies)
	}
	t.Setenv("TRUSTED_PROXY_CIDRS", "0.0.0.0/0,not-a-cidr")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CIDRs") {
		t.Fatalf("invalid trusted proxies error = %v", err)
	}
}

func TestLoadSessionCleanupSettings(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionCleanupInterval != 15*time.Minute || cfg.SessionCleanupBatchSize != 1000 {
		t.Fatalf("cleanup defaults = %v, %d", cfg.SessionCleanupInterval, cfg.SessionCleanupBatchSize)
	}
	t.Setenv("SESSION_CLEANUP_INTERVAL", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("zero cleanup interval was accepted")
	}
}

func writeSecretFile(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
