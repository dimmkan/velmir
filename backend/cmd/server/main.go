package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"velmir/backend/internal/config"
	"velmir/backend/internal/database"
	"velmir/backend/internal/httpapi"
	"velmir/backend/internal/introspection"
	"velmir/backend/internal/records"
)

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	adminDatabase, targetDatabase, targetWriteDatabase, err := database.NewPools(context.Background(), cfg.AdminDatabaseURL, cfg.TargetDatabaseURL, cfg.TargetWriteDatabaseURL)
	if err != nil {
		return err
	}
	defer adminDatabase.Close()
	if targetDatabase != nil {
		defer targetDatabase.Close()
	}
	if targetWriteDatabase != nil {
		defer targetWriteDatabase.Close()
	}

	var writer *records.Writer
	if targetWriteDatabase != nil {
		writer = records.NewWriter(targetWriteDatabase)
	}
	router := httpapi.NewRouterWithWriter(adminDatabase, nil, nil, nil, writer)
	if targetDatabase != nil {
		router = httpapi.NewRouterWithWriter(adminDatabase, targetDatabase, introspection.New(targetDatabase), records.New(targetDatabase), writer)
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverError := make(chan error, 1)
	go func() {
		serverError <- server.ListenAndServe()
	}()

	log.Printf("backend listening on %s", cfg.HTTPAddr)
	select {
	case err := <-serverError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-signalContext.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	if err := <-serverError; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}
