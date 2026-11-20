package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"velmir/backend/internal/auth"
)

const sessionCleanupTimeout = 10 * time.Second

type expiredSessionCleaner interface {
	CleanupExpiredSessions(context.Context, time.Time, int32) (int64, error)
}

func runMaintenance(ctx context.Context, store expiredSessionCleaner, authService *auth.Service, interval time.Duration, batchSize int32, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		authService.CleanupLoginAttempts()
		cleanupContext, cancel := context.WithTimeout(ctx, sessionCleanupTimeout)
		deleted, err := store.CleanupExpiredSessions(cleanupContext, time.Now(), batchSize)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("expired session cleanup failed", "error_type", fmt.Sprintf("%T", err))
			}
		} else if deleted > 0 {
			logger.Info("expired sessions removed", "count", deleted)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
