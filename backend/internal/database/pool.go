package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPools(ctx context.Context, adminDatabaseURL, targetDatabaseURL, targetWriteDatabaseURL string) (admin, target, targetWrite *pgxpool.Pool, err error) {
	admin, err = pgxpool.New(ctx, adminDatabaseURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create admin database pool: %w", err)
	}
	closeOnError := func(err error) (*pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool, error) {
		admin.Close()
		if target != nil {
			target.Close()
		}
		return nil, nil, nil, err
	}

	if targetDatabaseURL != "" {
		targetConfig, parseErr := pgxpool.ParseConfig(targetDatabaseURL)
		if parseErr != nil {
			return closeOnError(fmt.Errorf("parse target database configuration: %w", parseErr))
		}
		targetConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
		target, err = pgxpool.NewWithConfig(ctx, targetConfig)
		if err != nil {
			return closeOnError(fmt.Errorf("create target database pool: %w", err))
		}
	}

	if targetWriteDatabaseURL != "" {
		targetWrite, err = pgxpool.New(ctx, targetWriteDatabaseURL)
		if err != nil {
			return closeOnError(fmt.Errorf("create target write database pool: %w", err))
		}
	}
	return admin, target, targetWrite, nil
}
