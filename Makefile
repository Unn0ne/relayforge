.PHONY: build run test lint migrate-up migrate-down

build:
	go build -o bin/relayforge ./cmd/relayforge

run:
	go run ./cmd/relayforge

test:
	go test -race ./...

lint:
	golangci-lint run

migrate-up:
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_init.up.sql

migrate-down:
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_init.down.sql
