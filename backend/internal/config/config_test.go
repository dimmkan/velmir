package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("TARGET_DATABASE_URL", "postgres://target.example/test")
	t.Setenv("TARGET_WRITE_DATABASE_URL", "postgres://writer.example/test")
	t.Setenv("ADMIN_DATABASE_URL", "postgres://admin.example/test")
	t.Setenv("HTTP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TargetDatabaseURL != "postgres://target.example/test" {
		t.Errorf("TargetDatabaseURL = %q", cfg.TargetDatabaseURL)
	}
	if cfg.TargetWriteDatabaseURL != "postgres://writer.example/test" {
		t.Errorf("TargetWriteDatabaseURL = %q", cfg.TargetWriteDatabaseURL)
	}
	if cfg.AdminDatabaseURL != "postgres://admin.example/test" {
		t.Errorf("AdminDatabaseURL = %q", cfg.AdminDatabaseURL)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
}

func TestLoadCustomHTTPAddr(t *testing.T) {
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
	t.Setenv("TARGET_DATABASE_URL", "postgres://target.example/test")
	t.Setenv("ADMIN_DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want configuration error")
	}
}
