.PHONY: build run test test-integration lint vuln api-lint verify bench migrate-up migrate-down docker-build docker-config compose-up compose-down compose-logs

BINARY ?= bin/relayforge
COMPOSE ?= docker compose
ENV_FILE ?= .env
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= unknown
GO_BUILD_CGO ?= 0
GO_LDFLAGS := -X github.com/Unn0ne/relayforge/internal/buildinfo.Version=$(VERSION) -X github.com/Unn0ne/relayforge/internal/buildinfo.Commit=$(COMMIT) -X github.com/Unn0ne/relayforge/internal/buildinfo.BuiltAt=$(BUILD_DATE)

build:
	@mkdir -p "$(dir $(BINARY))"
	CGO_ENABLED=$(GO_BUILD_CGO) go build -trimpath -ldflags "$(GO_LDFLAGS)" -o "$(BINARY)" ./cmd/relayforge

run:
	CGO_ENABLED=$(GO_BUILD_CGO) go run ./cmd/relayforge

test:
	go test -race ./...

test-integration:
	@test -n "$(TEST_DATABASE_URL)" || (echo "TEST_DATABASE_URL is required" && exit 1)
	go test -race -count=1 ./internal/store ./internal/worker -run Integration

lint:
	golangci-lint run

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

api-lint:
	npx --yes @redocly/cli@2.51.2 lint openapi.yaml

verify: test lint vuln api-lint docker-config

bench:
	@test -n "$(ENDPOINT_ID)" || (echo "ENDPOINT_ID is required" && exit 1)
	CGO_ENABLED=$(GO_BUILD_CGO) go run ./cmd/relaybench -endpoint-id "$(ENDPOINT_ID)" $(BENCH_ARGS)

migrate-up:
	@psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_init.up.sql

migrate-down:
	@psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_init.down.sql

docker-build:
	docker build --build-arg VERSION="$(VERSION)" --build-arg COMMIT="$(COMMIT)" --build-arg BUILD_DATE="$(BUILD_DATE)" -t relayforge:local .

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
