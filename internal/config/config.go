package config

import (
	"encoding/base64"
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
	MasterKey              []byte
	APIKey                 string
	AllowHTTP              bool
	AllowPrivateTargets    bool
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

	masterKey, err := encryptionKey("RELAYFORGE_MASTER_KEY")
	if err != nil {
		return Config{}, err
	}

	apiKey, err := secret("RELAYFORGE_API_KEY", 32)
	if err != nil {
		return Config{}, err
	}

	allowHTTP, err := boolean("ALLOW_HTTP_TARGETS", false)
	if err != nil {
		return Config{}, err
	}

	allowPrivateTargets, err := boolean("ALLOW_PRIVATE_TARGETS", false)
	if err != nil {
		return Config{}, err
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
		MasterKey:              masterKey,
		APIKey:                 apiKey,
		AllowHTTP:              allowHTTP,
		AllowPrivateTargets:    allowPrivateTargets,
	}, nil
}

func secret(name string, minimumLength int) (string, error) {
	result := strings.TrimSpace(os.Getenv(name))
	if result == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if len(result) < minimumLength {
		return "", fmt.Errorf("%s must be at least %d characters", name, minimumLength)
	}
	return result, nil
}

func encryptionKey(name string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must decode to 32 bytes", name)
	}
	return key, nil
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

func boolean(key string, fallback bool) (bool, error) {
	raw := value(key, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
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
