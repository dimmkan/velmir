package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"velmir/backend/internal/config"
)

const migrationsDirectory = "migrations"

func main() {
	if err := run(); err != nil {
		log.Printf("migration failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	adminDatabase, err := sql.Open("pgx", cfg.AdminDatabaseURL)
	if err != nil {
		return fmt.Errorf("open admin database: %w", err)
	}
	defer adminDatabase.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set Goose dialect: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := goose.UpContext(ctx, adminDatabase, migrationsDirectory); err != nil {
		return fmt.Errorf("apply admin database migrations: %w", err)
	}
	return nil
}
