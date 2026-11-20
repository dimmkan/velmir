package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("TARGET_DATABASE_URL", "postgres://target.example/test")
	t.Setenv("TARGET_WRITE_DATABASE_URL", "")
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("HTTP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TargetDatabaseURL != "postgres://target.example/test" {
		t.Errorf("TargetDatabaseURL = %q", cfg.TargetDatabaseURL)
	}
	if cfg.AdminDatabaseURL != "postgres://admin.example/test" {
		t.Errorf("AdminDatabaseURL = %q", cfg.AdminDatabaseURL)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.HTTPReadHeaderTimeout != 5*time.Second || cfg.HTTPReadTimeout != 15*time.Second || cfg.HTTPWriteTimeout != 30*time.Second {
		t.Errorf("HTTP timeouts were not loaded: %+v", cfg)
	}
	if cfg.AdminDatabaseMaxConns != 10 || cfg.TargetDatabaseMaxConns != 10 || cfg.DatabaseConnectTimeout != 5*time.Second {
		t.Errorf("database defaults were not loaded: %+v", cfg)
	}
}

func TestLoadCustomHTTPAddr(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("TARGET_DATABASE_URL", "postgres://target.example/test")
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
}

func TestLoadAllowsMissingTargetDatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("TARGET_DATABASE_URL", "")
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("HTTP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TargetDatabaseURL != "" {
		t.Errorf("TargetDatabaseURL = %q, want empty", cfg.TargetDatabaseURL)
	}
}

func TestLoadRequiresAdminDatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("TARGET_DATABASE_URL", "postgres://target.example/test")
	t.Setenv("ADMIN_DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want configuration error")
	}
}

func TestLoadRejectsLegacyWriterDSN(t *testing.T) {
	for _, variable := range []string{"TARGET_WRITE_DATABASE_URL", "VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL"} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv("APP_ENV", Development)
			t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
			t.Setenv("TARGET_DATABASE_URL", "postgres://target.example/test")
			t.Setenv("TARGET_WRITE_DATABASE_URL", "")
			t.Setenv("VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL", "")
			t.Setenv(variable, "postgres://legacy-writer.example/test")

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "remove it and use TARGET_DATABASE_URL") {
				t.Fatalf("Load() error = %v, writer migration instructions", err)
			}
		})
	}
}

func TestLoadProductionSecurity(t *testing.T) {
	t.Setenv("APP_ENV", Production)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("FRONTEND_DIST_PATH", "/app/frontend/dist")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_COOKIE_SECURE=true") {
		t.Fatalf("insecure production error = %v", err)
	}

	t.Setenv("SESSION_COOKIE_SECURE", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SessionCookieSecure {
		t.Fatal("production secure cookie setting was not loaded")
	}
}
