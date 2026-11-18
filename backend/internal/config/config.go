package config

import (
	"errors"
	"os"
)

const defaultHTTPAddr = "127.0.0.1:8080"

type Config struct {
	TargetDatabaseURL string
	AdminDatabaseURL  string
	HTTPAddr          string
}

func Load() (Config, error) {
	targetDatabaseURL := os.Getenv("TARGET_DATABASE_URL")
	if targetDatabaseURL == "" {
		return Config{}, errors.New("TARGET_DATABASE_URL is required")
	}

	adminDatabaseURL := os.Getenv("ADMIN_DATABASE_URL")
	if adminDatabaseURL == "" {
		return Config{}, errors.New("ADMIN_DATABASE_URL is required")
	}

	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr
	}

	return Config{
		TargetDatabaseURL: targetDatabaseURL,
		AdminDatabaseURL:  adminDatabaseURL,
		HTTPAddr:          httpAddr,
	}, nil
}
