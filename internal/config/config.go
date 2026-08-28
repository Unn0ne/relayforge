package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                string
	ReadHeaderTimeout       time.Duration
	ShutdownTimeout         time.Duration
	LogLevel                slog.Level
	DatabaseURL             string
	DatabaseMinConnections  int32
	DatabaseMaxConnections  int32
	DatabaseConnectTimeout  time.Duration
	MasterKey               []byte
	APIKey                  string
	AllowHTTP               bool
	AllowPrivateTargets     bool
	WorkerConcurrency       int
	WorkerPollInterval      time.Duration
	WorkerLeaseDuration     time.Duration
	WorkerFinishTimeout     time.Duration
	RetryBaseDelay          time.Duration
	RetryMaxDelay           time.Duration
	RetryJitter             float64
	CircuitFailureThreshold int
	CircuitCooldown         time.Duration
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

	workerConcurrency, err := integer("WORKER_CONCURRENCY", 8)
	if err != nil {
		return Config{}, err
	}
	workerPollInterval, err := duration("WORKER_POLL_INTERVAL", 250*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	workerLeaseDuration, err := duration("WORKER_LEASE_DURATION", 45*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerFinishTimeout, err := duration("WORKER_FINISH_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	retryBaseDelay, err := duration("RETRY_BASE_DELAY", time.Second)
	if err != nil {
		return Config{}, err
	}
	retryMaxDelay, err := duration("RETRY_MAX_DELAY", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	retryJitter, err := decimal("RETRY_JITTER", 0.2)
	if err != nil {
		return Config{}, err
	}
	circuitFailureThreshold, err := integer("CIRCUIT_FAILURE_THRESHOLD", 5)
	if err != nil {
		return Config{}, err
	}
	circuitCooldown, err := duration("CIRCUIT_COOLDOWN", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	if workerConcurrency < 1 || workerConcurrency > 128 {
		return Config{}, errors.New("WORKER_CONCURRENCY must be between 1 and 128")
	}
	if workerLeaseDuration <= 30*time.Second+workerFinishTimeout {
		return Config{}, errors.New("WORKER_LEASE_DURATION must exceed the maximum endpoint timeout and finish timeout")
	}
	if retryMaxDelay < retryBaseDelay {
		return Config{}, errors.New("RETRY_MAX_DELAY must not be less than RETRY_BASE_DELAY")
	}
	if retryJitter < 0 || retryJitter > 1 {
		return Config{}, errors.New("RETRY_JITTER must be between 0 and 1")
	}
	if circuitFailureThreshold < 1 || circuitFailureThreshold > 100 {
		return Config{}, errors.New("CIRCUIT_FAILURE_THRESHOLD must be between 1 and 100")
	}

	return Config{
		HTTPAddr:                value("HTTP_ADDR", ":8080"),
		ReadHeaderTimeout:       readHeaderTimeout,
		ShutdownTimeout:         shutdownTimeout,
		LogLevel:                logLevel,
		DatabaseURL:             value("DATABASE_URL", "postgres://relayforge:relayforge@localhost:5432/relayforge?sslmode=disable"),
		DatabaseMinConnections:  int32(databaseMinConnections),
		DatabaseMaxConnections:  int32(databaseMaxConnections),
		DatabaseConnectTimeout:  databaseConnectTimeout,
		MasterKey:               masterKey,
		APIKey:                  apiKey,
		AllowHTTP:               allowHTTP,
		AllowPrivateTargets:     allowPrivateTargets,
		WorkerConcurrency:       workerConcurrency,
		WorkerPollInterval:      workerPollInterval,
		WorkerLeaseDuration:     workerLeaseDuration,
		WorkerFinishTimeout:     workerFinishTimeout,
		RetryBaseDelay:          retryBaseDelay,
		RetryMaxDelay:           retryMaxDelay,
		RetryJitter:             retryJitter,
		CircuitFailureThreshold: circuitFailureThreshold,
		CircuitCooldown:         circuitCooldown,
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

func decimal(key string, fallback float64) (float64, error) {
	raw := value(key, strconv.FormatFloat(fallback, 'f', -1, 64))
	parsed, err := strconv.ParseFloat(raw, 64)
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
