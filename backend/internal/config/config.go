package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

const defaultHTTPAddr = "127.0.0.1:8080"

type Config struct {
	TargetDatabaseURL string
	AdminDatabaseURL  string
	HTTPAddr          string
}

func Load() (Config, error) {
	if err := godotenv.Load(".env"); err != nil {
		_ = godotenv.Load("../.env")
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
		TargetDatabaseURL: os.Getenv("TARGET_DATABASE_URL"),
		AdminDatabaseURL:  adminDatabaseURL,
		HTTPAddr:          httpAddr,
	}, nil
}
