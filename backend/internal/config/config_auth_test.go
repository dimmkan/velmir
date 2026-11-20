package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAuthSettings(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("SESSION_IDLE_TIMEOUT", "10m")
	t.Setenv("SESSION_ABSOLUTE_TIMEOUT", "2h")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN", "admin")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD", "not-logged")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME", "First admin")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SessionCookieSecure || cfg.SessionIdleTimeout != 10*time.Minute || cfg.SessionAbsoluteTimeout != 2*time.Hour {
		t.Fatal("session settings were not loaded")
	}
	if cfg.BootstrapAdminLogin != "admin" || cfg.BootstrapAdminPassword != "not-logged" || cfg.BootstrapAdminDisplayName != "First admin" {
		t.Fatal("bootstrap settings were not loaded")
	}
}

func TestLoadBootstrapLegacyLogin(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN", "")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BootstrapAdminLogin != "admin@example.test" {
		t.Fatalf("legacy login = %q", cfg.BootstrapAdminLogin)
	}
}

func TestLoadRejectsConflictingBootstrapLogins(t *testing.T) {
	t.Setenv("APP_ENV", Development)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN", "admin")
	t.Setenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL", "other@example.test")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("Load() error = %v, want conflict", err)
	}
}

func TestLoadRejectsInvalidSessionSettings(t *testing.T) {
	for _, test := range []struct {
		name     string
		secure   string
		idle     string
		absolute string
	}{
		{name: "cookie flag", secure: "perhaps", idle: "30m", absolute: "24h"},
		{name: "idle duration", secure: "false", idle: "never", absolute: "24h"},
		{name: "absolute shorter than idle", secure: "false", idle: "2h", absolute: "1h"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("APP_ENV", Development)
			t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
			t.Setenv("SESSION_COOKIE_SECURE", test.secure)
			t.Setenv("SESSION_IDLE_TIMEOUT", test.idle)
			t.Setenv("SESSION_ABSOLUTE_TIMEOUT", test.absolute)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadRejectsProductionDemoMode(t *testing.T) {
	t.Setenv("APP_ENV", Production)
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("FRONTEND_DIST_PATH", "/app/frontend/dist")
	t.Setenv("VELMIR_DEMO_MODE", "true")
	if _, err := Load(); err == nil {
		t.Fatal("production demo mode was accepted")
	}
}
