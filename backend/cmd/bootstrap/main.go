package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/config"
	"velmir/backend/internal/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.NewAdminPool(ctx, database.PoolOptions{
		AdminDatabaseURL: cfg.AdminDatabaseURL,
		ConnectTimeout:   cfg.DatabaseConnectTimeout,
		AdminMaxConns:    cfg.AdminDatabaseMaxConns,
		MaxConnLifetime:  cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime:  cfg.DatabaseMaxConnIdleTime,
	})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	created, err := auth.Bootstrap(ctx, db, auth.BootstrapOptions{
		Login:       cfg.BootstrapAdminLogin,
		Password:    cfg.BootstrapAdminPassword,
		DisplayName: cfg.BootstrapAdminDisplayName,
		AppEnv:      cfg.AppEnv,
		DemoMode:    cfg.DemoMode,
	})
	if err != nil {
		return err
	}
	if !created {
		logger.Info("bootstrap skipped because users already exist")
		return nil
	}
	logger.Info("bootstrap administrator created", "login", cfg.BootstrapAdminLogin)
	if cfg.DemoMode {
		logger.Warn("demo bootstrap uses local-only default credentials; keep the demo on localhost and do not reuse them in production")
	} else {
		logger.Info("remove VELMIR_BOOTSTRAP_ADMIN_LOGIN, VELMIR_BOOTSTRAP_ADMIN_EMAIL and VELMIR_BOOTSTRAP_ADMIN_PASSWORD after the first production start")
	}
	return nil
}
