package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultHTTPAddr = "127.0.0.1:8080"
	Development     = "development"
	Production      = "production"
)

type Config struct {
	AppEnv                    string
	DemoMode                  bool
	TargetDatabaseURL         string
	AdminDatabaseURL          string
	HTTPAddr                  string
	HTTPReadHeaderTimeout     time.Duration
	HTTPReadTimeout           time.Duration
	HTTPWriteTimeout          time.Duration
	HTTPIdleTimeout           time.Duration
	HTTPShutdownTimeout       time.Duration
	HTTPMaxJSONBodySize       int64
	FrontendDistPath          string
	LogLevel                  string
	BootstrapAdminLogin       string
	BootstrapAdminPassword    string
	BootstrapAdminDisplayName string
	SessionCookieSecure       bool
	SessionIdleTimeout        time.Duration
	SessionAbsoluteTimeout    time.Duration
	DatabaseConnectTimeout    time.Duration
	AdminDatabaseMaxConns     int32
	TargetDatabaseMaxConns    int32
	DatabaseMaxConnLifetime   time.Duration
	DatabaseMaxConnIdleTime   time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(".env"); err != nil {
		_ = godotenv.Load("../.env")
	}

	if os.Getenv("TARGET_WRITE_DATABASE_URL") != "" || os.Getenv("VELMIR_DEPRECATED_TARGET_WRITE_DATABASE_URL") != "" {
		return Config{}, errors.New("TARGET_WRITE_DATABASE_URL is no longer supported; remove it and use TARGET_DATABASE_URL for both reads and supported mutations, with privileges granted by the target database owner")
	}

	adminDatabaseURL := os.Getenv("ADMIN_DATABASE_URL")
	if adminDatabaseURL == "" {
		return Config{}, errors.New("ADMIN_DATABASE_URL is required")
	}
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if appEnv == "" {
		appEnv = Development
	}
	if appEnv != Development && appEnv != Production {
		return Config{}, errors.New("APP_ENV must be development or production")
	}
	demoMode, err := boolFromEnv("VELMIR_DEMO_MODE", false)
	if err != nil {
		return Config{}, err
	}
	if demoMode && appEnv != Development {
		return Config{}, errors.New("VELMIR_DEMO_MODE=true requires APP_ENV=development and must never be used in production")
	}

	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr
	}
	readHeaderTimeout, err := durationFromEnv("HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	readTimeout, err := durationFromEnv("HTTP_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := durationFromEnv("HTTP_WRITE_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationFromEnv("HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationFromEnv("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	for name, value := range map[string]time.Duration{
		"HTTP_READ_HEADER_TIMEOUT": readHeaderTimeout,
		"HTTP_READ_TIMEOUT":        readTimeout,
		"HTTP_WRITE_TIMEOUT":       writeTimeout,
		"HTTP_IDLE_TIMEOUT":        idleTimeout,
		"HTTP_SHUTDOWN_TIMEOUT":    shutdownTimeout,
	} {
		if value <= 0 {
			return Config{}, fmt.Errorf("%s must be positive", name)
		}
	}
	maxJSONBodySize, err := int64FromEnv("HTTP_MAX_JSON_BODY_SIZE", 1024*1024)
	if err != nil || maxJSONBodySize <= 0 {
		return Config{}, errors.New("HTTP_MAX_JSON_BODY_SIZE must be a positive number of bytes")
	}

	cookieSecure := false
	if value := os.Getenv("SESSION_COOKIE_SECURE"); value != "" {
		cookieSecure, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, errors.New("SESSION_COOKIE_SECURE must be true or false")
		}
	}
	if appEnv == Production && !cookieSecure {
		return Config{}, errors.New("SESSION_COOKIE_SECURE=true is required when APP_ENV=production; terminate HTTPS before Velmir")
	}
	sessionIdleTimeout, err := durationFromEnv("SESSION_IDLE_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	sessionAbsoluteTimeout, err := durationFromEnv("SESSION_ABSOLUTE_TIMEOUT", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if sessionIdleTimeout <= 0 || sessionAbsoluteTimeout < sessionIdleTimeout {
		return Config{}, errors.New("session timeouts must be positive and SESSION_ABSOLUTE_TIMEOUT must be at least SESSION_IDLE_TIMEOUT")
	}

	databaseConnectTimeout, err := durationFromEnv("DATABASE_CONNECT_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	databaseMaxConnLifetime, err := durationFromEnv("DATABASE_MAX_CONN_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	databaseMaxConnIdleTime, err := durationFromEnv("DATABASE_MAX_CONN_IDLE_TIME", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if databaseConnectTimeout <= 0 || databaseMaxConnLifetime <= 0 || databaseMaxConnIdleTime <= 0 {
		return Config{}, errors.New("database connection timeouts and connection lifetimes must be positive")
	}
	adminMaxConns, err := int32FromEnv("ADMIN_DATABASE_MAX_CONNS", 10)
	if err != nil || adminMaxConns <= 0 {
		return Config{}, errors.New("ADMIN_DATABASE_MAX_CONNS must be a positive integer")
	}
	targetMaxConns, err := int32FromEnv("TARGET_DATABASE_MAX_CONNS", 10)
	if err != nil || targetMaxConns <= 0 {
		return Config{}, errors.New("TARGET_DATABASE_MAX_CONNS must be a positive integer")
	}

	login := strings.TrimSpace(os.Getenv("VELMIR_BOOTSTRAP_ADMIN_LOGIN"))
	legacyLogin := strings.TrimSpace(os.Getenv("VELMIR_BOOTSTRAP_ADMIN_EMAIL"))
	if login != "" && legacyLogin != "" && !strings.EqualFold(login, legacyLogin) {
		return Config{}, errors.New("VELMIR_BOOTSTRAP_ADMIN_LOGIN and VELMIR_BOOTSTRAP_ADMIN_EMAIL conflict; remove the legacy email variable or make both values identical")
	}
	if login == "" {
		login = legacyLogin
	}
	displayName := strings.TrimSpace(os.Getenv("VELMIR_BOOTSTRAP_ADMIN_DISPLAY_NAME"))
	if displayName == "" {
		displayName = "Administrator"
	}
	logLevel := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	if logLevel == "" {
		logLevel = "info"
	}
	switch logLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn or error")
	}
	frontendDistPath := strings.TrimSpace(os.Getenv("FRONTEND_DIST_PATH"))
	if appEnv == Production && frontendDistPath == "" {
		return Config{}, errors.New("FRONTEND_DIST_PATH is required when APP_ENV=production")
	}

	return Config{
		AppEnv:                    appEnv,
		DemoMode:                  demoMode,
		TargetDatabaseURL:         os.Getenv("TARGET_DATABASE_URL"),
		AdminDatabaseURL:          adminDatabaseURL,
		HTTPAddr:                  httpAddr,
		HTTPReadHeaderTimeout:     readHeaderTimeout,
		HTTPReadTimeout:           readTimeout,
		HTTPWriteTimeout:          writeTimeout,
		HTTPIdleTimeout:           idleTimeout,
		HTTPShutdownTimeout:       shutdownTimeout,
		HTTPMaxJSONBodySize:       maxJSONBodySize,
		FrontendDistPath:          frontendDistPath,
		LogLevel:                  logLevel,
		BootstrapAdminLogin:       login,
		BootstrapAdminPassword:    os.Getenv("VELMIR_BOOTSTRAP_ADMIN_PASSWORD"),
		BootstrapAdminDisplayName: displayName,
		SessionCookieSecure:       cookieSecure,
		SessionIdleTimeout:        sessionIdleTimeout,
		SessionAbsoluteTimeout:    sessionAbsoluteTimeout,
		DatabaseConnectTimeout:    databaseConnectTimeout,
		AdminDatabaseMaxConns:     adminMaxConns,
		TargetDatabaseMaxConns:    targetMaxConns,
		DatabaseMaxConnLifetime:   databaseMaxConnLifetime,
		DatabaseMaxConnIdleTime:   databaseMaxConnIdleTime,
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

func boolFromEnv(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func int64FromEnv(name string, fallback int64) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func int32FromEnv(name string, fallback int32) (int32, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	return int32(parsed), err
}
