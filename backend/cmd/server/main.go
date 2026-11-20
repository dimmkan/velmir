package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"velmir/backend/internal/audit"
	"velmir/backend/internal/auth"
	"velmir/backend/internal/authorization"
	"velmir/backend/internal/config"
	"velmir/backend/internal/database"
	"velmir/backend/internal/httpapi"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("parse log level: %w", err)
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	poolOptions := database.PoolOptions{
		AdminDatabaseURL:  cfg.AdminDatabaseURL,
		TargetDatabaseURL: cfg.TargetDatabaseURL,
		ConnectTimeout:    cfg.DatabaseConnectTimeout,
		AdminMaxConns:     cfg.AdminDatabaseMaxConns,
		TargetMaxConns:    cfg.TargetDatabaseMaxConns,
		MaxConnLifetime:   cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime:   cfg.DatabaseMaxConnIdleTime,
	}
	adminDatabase, targetDatabase, targetMutationDatabase, err := database.NewPools(context.Background(), poolOptions)
	if err != nil {
		return err
	}
	defer adminDatabase.Close()
	if targetDatabase != nil {
		defer targetDatabase.Close()
	}
	if targetMutationDatabase != nil {
		defer targetMutationDatabase.Close()
	}
	startupContext, cancelStartup := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	if err := adminDatabase.Ping(startupContext); err != nil {
		cancelStartup()
		return fmt.Errorf("connect to admin database: %w", err)
	}
	cancelStartup()

	authService, err := auth.NewService(auth.NewStore(adminDatabase), cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout)
	if err != nil {
		return fmt.Errorf("initialize authentication: %w", err)
	}
	var writer *records.Writer
	if targetMutationDatabase != nil {
		writer = records.NewWriter(targetMutationDatabase)
	}
	permissionStore := authorization.NewStore(adminDatabase)
	administration := auth.NewAdministration(adminDatabase)
	auditStore := audit.NewStore(adminDatabase)
	router := httpapi.NewRouter(adminDatabase, nil, nil, nil, writer, authService, permissionStore, auditStore, cfg.SessionCookieSecure, administration)
	if targetDatabase != nil {
		router = httpapi.NewRouter(adminDatabase, targetDatabase, introspection.New(targetDatabase), records.New(targetDatabase), writer, authService, permissionStore, auditStore, cfg.SessionCookieSecure, administration)
	}
	var handler http.Handler = httpapi.LimitJSONBody(router, cfg.HTTPMaxJSONBodySize)
	if cfg.FrontendDistPath != "" {
		handler, err = httpapi.NewStaticHandler(handler, cfg.FrontendDistPath)
		if err != nil {
			return fmt.Errorf("configure frontend static files: %w", err)
		}
	} else {
		logger.Info("frontend static serving is disabled; use the Vite development server")
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverError := make(chan error, 1)
	go func() {
		serverError <- server.ListenAndServe()
	}()
	logger.Info("backend listening", "address", cfg.HTTPAddr, "app_env", cfg.AppEnv, "target_configured", cfg.TargetDatabaseURL != "")
	select {
	case err := <-serverError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-signalContext.Done():
		logger.Info("shutdown signal received")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	if err := <-serverError; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	logger.Info("server stopped gracefully")
	return nil
}
