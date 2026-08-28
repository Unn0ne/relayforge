.PHONY: build run test lint

build:
	go build -o bin/relayforge ./cmd/relayforge

run:
	go run ./cmd/relayforge

test:
	go test -race ./...

lint:
	golangci-lint run

