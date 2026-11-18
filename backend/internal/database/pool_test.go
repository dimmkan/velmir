package database

import (
	"context"
	"testing"
)

func TestNewPoolsWithoutTarget(t *testing.T) {
	admin, target, targetWrite, err := NewPools(context.Background(), "postgres://admin.example/test", "", "")
	if err != nil {
		t.Fatalf("NewPools() error = %v", err)
	}
	defer admin.Close()

	if admin == nil {
		t.Fatal("admin pool = nil")
	}
	if target != nil || targetWrite != nil {
		t.Fatal("target pools are not nil")
	}
}

func TestNewPoolsWithSeparateTargets(t *testing.T) {
	admin, target, targetWrite, err := NewPools(context.Background(), "postgres://admin.example/test", "postgres://target.example/test", "postgres://writer.example/test")
	if err != nil {
		t.Fatalf("NewPools() error = %v", err)
	}
	defer admin.Close()
	defer target.Close()
	defer targetWrite.Close()

	if admin == target || target == targetWrite {
		t.Fatal("database pools must be separate")
	}
	if got := target.Config().ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "on" {
		t.Errorf("target default_transaction_read_only = %q, want on", got)
	}
	if got := targetWrite.Config().ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "" {
		t.Errorf("write target unexpectedly overrides default_transaction_read_only = %q", got)
	}
}

func TestNewPoolsRejectsInvalidAdminURL(t *testing.T) {
	if _, _, _, err := NewPools(context.Background(), "://invalid", "", ""); err == nil {
		t.Fatal("NewPools() error = nil")
	}
}
