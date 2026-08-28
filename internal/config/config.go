package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr               string
	ReadHeaderTimeout      time.Duration
	ShutdownTimeout        time.Duration
	LogLevel               slog.Level
	DatabaseURL            string
	DatabaseMinConnections int32
	DatabaseMaxConnections int32
	DatabaseConnectTimeout time.Duration
}

func Load() (Config, error) {
	readHeaderTimeout, err := duration("READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := duration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	logLevel, err := level(value("LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}

	databaseConnectTimeout, err := duration("DATABASE_CONNECT_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	databaseMinConnections, err := integer("DATABASE_MIN_CONNECTIONS", 1)
	if err != nil {
		return Config{}, err
	}

	databaseMaxConnections, err := integer("DATABASE_MAX_CONNECTIONS", 10)
	if err != nil {
		return Config{}, err
	}

	if databaseMinConnections < 0 {
		return Config{}, fmt.Errorf("DATABASE_MIN_CONNECTIONS must not be negative")
	}
	if databaseMaxConnections < 1 {
		return Config{}, fmt.Errorf("DATABASE_MAX_CONNECTIONS must be positive")
	}
	if databaseMinConnections > databaseMaxConnections {
		return Config{}, fmt.Errorf("DATABASE_MIN_CONNECTIONS must not exceed DATABASE_MAX_CONNECTIONS")
	}

	return Config{
		HTTPAddr:               value("HTTP_ADDR", ":8080"),
		ReadHeaderTimeout:      readHeaderTimeout,
		ShutdownTimeout:        shutdownTimeout,
		LogLevel:               logLevel,
		DatabaseURL:            value("DATABASE_URL", "postgres://relayforge:relayforge@localhost:5432/relayforge?sslmode=disable"),
		DatabaseMinConnections: int32(databaseMinConnections),
		DatabaseMaxConnections: int32(databaseMaxConnections),
		DatabaseConnectTimeout: databaseConnectTimeout,
	}, nil
}

func value(key, fallback string) string {
	if current := strings.TrimSpace(os.Getenv(key)); current != "" {
		return current
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := value(key, fallback.String())
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return parsed, nil
}

func integer(key string, fallback int) (int, error) {
	raw := value(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func level(raw string) (slog.Level, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("parse LOG_LEVEL: %w", err)
	}
	return parsed, nil
}
