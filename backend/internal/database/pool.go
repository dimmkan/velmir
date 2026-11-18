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

	target, err = pgxpool.New(ctx, targetDatabaseURL)
	if err != nil {
		admin.Close()
		return nil, nil, fmt.Errorf("create target database pool: %w", err)
	}

	return admin, target, nil
}
