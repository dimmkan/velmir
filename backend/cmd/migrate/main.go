package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"velmir/backend/internal/config"
)

const migrationsDirectory = "migrations"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("admin database migrations applied")
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	connectionConfig, err := pgx.ParseConfig(cfg.AdminDatabaseURL)
	if err != nil {
		return errors.New("invalid ADMIN_DATABASE_URL")
	}
	connectionConfig.ConnectTimeout = cfg.DatabaseConnectTimeout
	adminDatabase := stdlib.OpenDB(*connectionConfig)
	defer adminDatabase.Close()
	adminDatabase.SetMaxOpenConns(int(cfg.AdminDatabaseMaxConns))
	adminDatabase.SetConnMaxLifetime(cfg.DatabaseMaxConnLifetime)
	adminDatabase.SetConnMaxIdleTime(cfg.DatabaseMaxConnIdleTime)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set Goose dialect: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := goose.UpContext(ctx, adminDatabase, migrationsDirectory); err != nil {
		return fmt.Errorf("apply admin database migrations: %w", err)
	}
	return nil
}
