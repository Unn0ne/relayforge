package config

import (
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
}
