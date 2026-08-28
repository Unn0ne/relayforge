package config

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("DATABASE_MIN_CONNECTIONS", "2")
	t.Setenv("DATABASE_MAX_CONNECTIONS", "12")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.DatabaseMinConnections != 2 {
		t.Fatalf("DatabaseMinConnections = %d", cfg.DatabaseMinConnections)
	}
	if cfg.DatabaseMaxConnections != 12 {
		t.Fatalf("DatabaseMaxConnections = %d", cfg.DatabaseMaxConnections)
	}
	if cfg.DatabaseConnectTimeout != 3*time.Second {
		t.Fatalf("DatabaseConnectTimeout = %s", cfg.DatabaseConnectTimeout)
	}
	if len(cfg.MasterKey) != 32 {
		t.Fatalf("MasterKey length = %d", len(cfg.MasterKey))
	}
	if cfg.APIKey != "01234567890123456789012345678901" {
		t.Fatalf("APIKey = %q", cfg.APIKey)
	}
	if cfg.WorkerConcurrency != 8 || cfg.WorkerLeaseDuration != 45*time.Second {
		t.Fatalf("worker config = %+v", cfg)
	}
}

func TestLoadRejectsInvalidPoolRange(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("DATABASE_MIN_CONNECTIONS", "8")
	t.Setenv("DATABASE_MAX_CONNECTIONS", "4")

	if _, err := Load(); err == nil {
		t.Fatal("expected pool range validation error")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("SHUTDOWN_TIMEOUT", "never")

	if _, err := Load(); err == nil {
		t.Fatal("expected duration validation error")
	}
}

func TestLoadRejectsMissingMasterKey(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("RELAYFORGE_MASTER_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestLoadRejectsInvalidMasterKey(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("RELAYFORGE_MASTER_KEY", base64.StdEncoding.EncodeToString([]byte("short")))

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestLoadRejectsShortAPIKey(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("RELAYFORGE_API_KEY", "short")

	if _, err := Load(); err == nil {
		t.Fatal("expected short API key error")
	}
}

func TestLoadRejectsInvalidBoolean(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("ALLOW_HTTP_TARGETS", "sometimes")

	if _, err := Load(); err == nil {
		t.Fatal("expected boolean validation error")
	}
}

func TestLoadRejectsShortWorkerLease(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("WORKER_LEASE_DURATION", "30s")

	if _, err := Load(); err == nil {
		t.Fatal("expected worker lease validation error")
	}
}

func TestLoadRejectsInvalidRetryJitter(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("RETRY_JITTER", "1.5")

	if _, err := Load(); err == nil {
		t.Fatal("expected retry jitter validation error")
	}
}

func setValidEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("READ_HEADER_TIMEOUT", "5s")
	t.Setenv("SHUTDOWN_TIMEOUT", "10s")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("DATABASE_URL", "postgres://relayforge:relayforge@localhost:5432/relayforge?sslmode=disable")
	t.Setenv("DATABASE_MIN_CONNECTIONS", "1")
	t.Setenv("DATABASE_MAX_CONNECTIONS", "10")
	t.Setenv("DATABASE_CONNECT_TIMEOUT", "3s")
	t.Setenv("RELAYFORGE_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("RELAYFORGE_API_KEY", "01234567890123456789012345678901")
	t.Setenv("ALLOW_HTTP_TARGETS", "false")
	t.Setenv("ALLOW_PRIVATE_TARGETS", "false")
	t.Setenv("WORKER_CONCURRENCY", "8")
	t.Setenv("WORKER_POLL_INTERVAL", "250ms")
	t.Setenv("WORKER_LEASE_DURATION", "45s")
	t.Setenv("WORKER_FINISH_TIMEOUT", "5s")
	t.Setenv("RETRY_BASE_DELAY", "1s")
	t.Setenv("RETRY_MAX_DELAY", "5m")
	t.Setenv("RETRY_JITTER", "0.2")
	t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "5")
	t.Setenv("CIRCUIT_COOLDOWN", "30s")
}
