package database

import (
	"context"
	"testing"
)

func TestNewPoolsWithoutTarget(t *testing.T) {
	admin, target, err := NewPools(context.Background(), "postgres://admin.example/test", "")
	if err != nil {
		t.Fatalf("NewPools() error = %v", err)
	}
	defer admin.Close()

	if admin == nil {
		t.Fatal("admin pool = nil")
	}
	if target != nil {
		t.Fatal("target pool is not nil")
	}
}

func TestNewPoolsWithSeparateTargets(t *testing.T) {
	admin, target, err := NewPools(context.Background(), "postgres://admin.example/test", "postgres://target.example/test")
	if err != nil {
		t.Fatalf("NewPools() error = %v", err)
	}
	defer admin.Close()
	defer target.Close()

	if admin == target {
		t.Fatal("admin and target pools are the same")
	}
	if got := target.Config().ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "on" {
		t.Errorf("target default_transaction_read_only = %q, want on", got)
	}
}

func TestNewPoolsRejectsInvalidAdminURL(t *testing.T) {
	if _, _, err := NewPools(context.Background(), "://invalid", ""); err == nil {
		t.Fatal("NewPools() error = nil")
	}
}
