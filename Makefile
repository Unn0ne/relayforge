.PHONY: build run test test-integration lint api-lint verify bench migrate-up migrate-down docker-build docker-config compose-up compose-down compose-logs

BINARY ?= bin/relayforge
COMPOSE ?= docker compose
ENV_FILE ?= .env

build:
	@mkdir -p "$(dir $(BINARY))"
	go build -trimpath -o "$(BINARY)" ./cmd/relayforge

run:
	go run ./cmd/relayforge

test:
	go test -race ./...

test-integration:
	@test -n "$(TEST_DATABASE_URL)" || (echo "TEST_DATABASE_URL is required" && exit 1)
	go test -race -count=1 ./internal/store ./internal/worker -run Integration

lint:
	golangci-lint run

api-lint:
	npx --yes @redocly/cli@2.51.1 lint openapi.yaml

verify: test lint api-lint docker-config

bench:
	@test -n "$(ENDPOINT_ID)" || (echo "ENDPOINT_ID is required" && exit 1)
	go run ./cmd/relaybench -endpoint-id "$(ENDPOINT_ID)" $(BENCH_ARGS)

migrate-up:
	@psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_init.up.sql

migrate-down:
	@psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_init.down.sql

docker-build:
	docker build -t relayforge:local .

docker-config:
	$(COMPOSE) --env-file .env.example config --quiet

compose-up:
	@test -f "$(ENV_FILE)" || (echo "$(ENV_FILE) is missing; copy .env.example first" && exit 1)
	$(COMPOSE) --env-file "$(ENV_FILE)" up -d --build

compose-down:
	@test -f "$(ENV_FILE)" || (echo "$(ENV_FILE) is missing" && exit 1)
	$(COMPOSE) --env-file "$(ENV_FILE)" down

compose-logs:
	@test -f "$(ENV_FILE)" || (echo "$(ENV_FILE) is missing" && exit 1)
	$(COMPOSE) --env-file "$(ENV_FILE)" logs -f relayforge
