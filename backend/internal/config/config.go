package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const defaultHTTPAddr = "127.0.0.1:8080"

type Config struct {
	TargetDatabaseURL      string
	TargetWriteDatabaseURL string
	AdminDatabaseURL       string
	HTTPAddr               string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
	SessionCookieSecure    bool
	SessionIdleTimeout     time.Duration
	SessionAbsoluteTimeout time.Duration
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
	cookieSecure := false
	if value := os.Getenv("SESSION_COOKIE_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, errors.New("SESSION_COOKIE_SECURE must be true or false")
		}
		cookieSecure = parsed
	}
	idleTimeout, err := durationFromEnv("SESSION_IDLE_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	absoluteTimeout, err := durationFromEnv("SESSION_ABSOLUTE_TIMEOUT", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if idleTimeout <= 0 || absoluteTimeout < idleTimeout {
		return Config{}, errors.New("session timeouts must be positive and SESSION_ABSOLUTE_TIMEOUT must be at least SESSION_IDLE_TIMEOUT")
	}

	return Config{
		TargetDatabaseURL:      os.Getenv("TARGET_DATABASE_URL"),
		TargetWriteDatabaseURL: os.Getenv("TARGET_WRITE_DATABASE_URL"),
		AdminDatabaseURL:       adminDatabaseURL,
		HTTPAddr:               httpAddr,
		BootstrapAdminEmail:    os.Getenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL"),
		BootstrapAdminPassword: os.Getenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD"),
		SessionCookieSecure:    cookieSecure,
		SessionIdleTimeout:     idleTimeout,
		SessionAbsoluteTimeout: absoluteTimeout,
	}, nil
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid Go duration", name)
	}
	return parsed, nil
}
