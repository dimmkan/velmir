package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolOptions struct {
	AdminDatabaseURL  string
	TargetDatabaseURL string
	ConnectTimeout    time.Duration
	AdminMaxConns     int32
	TargetMaxConns    int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
}

func NewAdminPool(ctx context.Context, options PoolOptions) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(options.AdminDatabaseURL)
	if err != nil {
		return nil, errors.New("invalid ADMIN_DATABASE_URL")
	}
	applyPoolOptions(config, options.ConnectTimeout, options.AdminMaxConns, options.MaxConnLifetime, options.MaxConnIdleTime)
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("create admin database pool failed")
	}
	return admin, nil
}

func NewPools(ctx context.Context, options PoolOptions) (admin, targetRead, targetMutation *pgxpool.Pool, err error) {
	admin, err = NewAdminPool(ctx, options)
	if err != nil {
		return nil, nil, nil, err
	}
	closeOnError := func(err error) (*pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool, error) {
		admin.Close()
		if targetRead != nil {
			targetRead.Close()
		}
		return nil, nil, nil, err
	}

	if options.TargetDatabaseURL == "" {
		return admin, nil, nil, nil
	}
	targetRead, err = newTargetPool(ctx, options, true)
	if err != nil {
		return closeOnError(err)
	}
	targetMutation, err = newTargetPool(ctx, options, false)
	if err != nil {
		return closeOnError(err)
	}
	return admin, targetRead, targetMutation, nil
}

func newTargetPool(ctx context.Context, options PoolOptions, readOnly bool) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(options.TargetDatabaseURL)
	if err != nil {
		return nil, errors.New("invalid TARGET_DATABASE_URL")
	}
	applyPoolOptions(config, options.ConnectTimeout, options.TargetMaxConns, options.MaxConnLifetime, options.MaxConnIdleTime)
	if readOnly {
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("create target database pool failed")
	}
	return pool, nil
}

func applyPoolOptions(config *pgxpool.Config, connectTimeout time.Duration, maxConns int32, maxLifetime, maxIdleTime time.Duration) {
	config.ConnConfig.ConnectTimeout = connectTimeout
	config.MaxConns = maxConns
	config.MaxConnLifetime = maxLifetime
	config.MaxConnIdleTime = maxIdleTime
}
