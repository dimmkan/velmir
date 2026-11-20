package database

import (
	"context"
	"testing"
	"time"
)

func TestNewPoolsWithoutTarget(t *testing.T) {
	admin, targetRead, targetMutation, err := NewPools(context.Background(), testPoolOptions(""))
	if err != nil {
		t.Fatalf("NewPools() error = %v", err)
	}
	defer admin.Close()
	if admin == nil {
		t.Fatal("admin pool = nil")
	}
	if targetRead != nil || targetMutation != nil {
		t.Fatal("target pools are not nil")
	}
}

func TestNewPoolsWithOneTargetDSN(t *testing.T) {
	admin, targetRead, targetMutation, err := NewPools(context.Background(), testPoolOptions("postgres://target.example/test"))
	if err != nil {
		t.Fatalf("NewPools() error = %v", err)
	}
	defer admin.Close()
	defer targetRead.Close()
	defer targetMutation.Close()
	if admin == targetRead || targetRead == targetMutation {
		t.Fatal("database pools must be separate")
	}
	if got := targetRead.Config().ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "on" {
		t.Errorf("target read default_transaction_read_only = %q, want on", got)
	}
	if got := targetMutation.Config().ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "" {
		t.Errorf("target mutation unexpectedly overrides default_transaction_read_only = %q", got)
	}
	if targetRead.Config().ConnConfig.Host != targetMutation.Config().ConnConfig.Host || targetRead.Config().ConnConfig.Database != targetMutation.Config().ConnConfig.Database {
		t.Fatal("read and mutation pools do not use the same target DSN")
	}
	if admin.Config().MaxConns != 3 || targetRead.Config().MaxConns != 4 || targetMutation.Config().MaxConns != 4 {
		t.Fatal("pool limits were not applied")
	}
	if admin.Config().ConnConfig.ConnectTimeout != 2*time.Second || targetRead.Config().MaxConnLifetime != 20*time.Minute || targetMutation.Config().MaxConnIdleTime != 4*time.Minute {
		t.Fatal("pool timeouts were not applied")
	}
}

func TestNewPoolsRejectsInvalidAdminURL(t *testing.T) {
	options := testPoolOptions("")
	options.AdminDatabaseURL = "://invalid"
	if _, _, _, err := NewPools(context.Background(), options); err == nil {
		t.Fatal("NewPools() error = nil")
	}
}

func testPoolOptions(targetURL string) PoolOptions {
	return PoolOptions{
		AdminDatabaseURL:  "postgres://admin.example/test",
		TargetDatabaseURL: targetURL,
		ConnectTimeout:    2 * time.Second,
		AdminMaxConns:     3,
		TargetMaxConns:    4,
		MaxConnLifetime:   20 * time.Minute,
		MaxConnIdleTime:   4 * time.Minute,
	}
}
