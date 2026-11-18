package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPools(ctx context.Context, adminDatabaseURL, targetDatabaseURL string) (admin, target *pgxpool.Pool, err error) {
	admin, err = pgxpool.New(ctx, adminDatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("create admin database pool: %w", err)
	}

	if targetDatabaseURL == "" {
		return admin, nil, nil
	}

	targetConfig, err := pgxpool.ParseConfig(targetDatabaseURL)
	if err != nil {
		admin.Close()
		return nil, nil, fmt.Errorf("parse target database configuration: %w", err)
	}
	targetConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"

	target, err = pgxpool.NewWithConfig(ctx, targetConfig)
	if err != nil {
		admin.Close()
		return nil, nil, fmt.Errorf("create target database pool: %w", err)
	}

	return admin, target, nil
}
