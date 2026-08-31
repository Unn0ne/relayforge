# RelayForge

RelayForge is a reliable webhook delivery service written in Go. It accepts events once, delivers them asynchronously, and makes failed deliveries observable and recoverable.

The project is built as a production-oriented portfolio service rather than a framework demo. The implementation is developed in small, verified increments.

## Capabilities

- idempotent event ingestion
- durable PostgreSQL-backed delivery queue
- concurrent workers with leases and `SKIP LOCKED`
- exponential backoff with jitter and dead-letter handling
- HMAC request signing
- encrypted endpoint secrets
- SSRF-resistant outbound requests
- endpoint circuit breaking
- Prometheus metrics and structured logs
- versioned migrations and a hardened Docker Compose environment

## Current state

The service exposes liveness, database-backed readiness, and Prometheus endpoints, manages a bounded PostgreSQL connection pool, and supports graceful shutdown. Concurrent workers claim deliveries through `FOR UPDATE SKIP LOCKED`, send signed requests through an SSRF-safe transport, persist immutable attempts, schedule jittered retries, and maintain endpoint circuit state.

## Quick start with Docker

```bash
cp .env.example .env
make compose-up
curl http://localhost:8080/health/ready
curl http://localhost:8080/metrics
```

The credentials in `.env.example` are only for an isolated local environment. Replace both RelayForge keys before using the stack on a shared machine. Migrations run in a one-shot container and are safe to execute again against the same volume.

```bash
make compose-logs
make compose-down
```

## Run from source

```bash
export RELAYFORGE_MASTER_KEY="$(openssl rand -base64 32)"
export RELAYFORGE_API_KEY="$(openssl rand -hex 32)"
make run
curl http://localhost:8080/health/ready
```

PostgreSQL must already be running and migration `001` must be applied when the service is run outside Compose.

## Configuration

| Variable | Default |
| --- | --- |
| `HTTP_ADDR` | `:8080` |
| `READ_HEADER_TIMEOUT` | `5s` |
| `SHUTDOWN_TIMEOUT` | `10s` |
| `LOG_LEVEL` | `info` |
| `DATABASE_URL` | `postgres://relayforge:relayforge@localhost:5432/relayforge?sslmode=disable` |
| `DATABASE_MIN_CONNECTIONS` | `1` |
| `DATABASE_MAX_CONNECTIONS` | `10` |
| `DATABASE_CONNECT_TIMEOUT` | `5s` |
| `RELAYFORGE_MASTER_KEY` | required 32-byte key encoded as base64 |
| `RELAYFORGE_API_KEY` | required bearer token with at least 32 characters |
| `ALLOW_HTTP_TARGETS` | `false` |
| `ALLOW_PRIVATE_TARGETS` | `false` |
| `WORKER_CONCURRENCY` | `8` |
| `WORKER_POLL_INTERVAL` | `250ms` |
| `WORKER_LEASE_DURATION` | `45s` |
| `WORKER_FINISH_TIMEOUT` | `5s` |
| `RETRY_BASE_DELAY` | `1s` |
| `RETRY_MAX_DELAY` | `5m` |
| `RETRY_JITTER` | `0.2` |
| `CIRCUIT_FAILURE_THRESHOLD` | `5` |
| `CIRCUIT_COOLDOWN` | `30s` |

## Endpoint API

```bash
curl -X POST http://localhost:8080/v1/endpoints \
  -H "Authorization: Bearer $RELAYFORGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"billing","url":"https://example.com/webhooks"}'
```

The generated signing secret is returned only by the create operation. Subsequent reads never expose the encrypted value.

## Event API

```bash
curl -X POST http://localhost:8080/v1/events \
  -H "Authorization: Bearer $RELAYFORGE_API_KEY" \
  -H "Idempotency-Key: invoice-123-paid" \
  -H "Content-Type: application/json" \
  -d '{"endpoint_id":"<endpoint-id>","type":"invoice.paid","payload":{"id":"invoice-123"}}'
```

Reusing an idempotency key with the same event returns the original event and delivery IDs. Reusing it with a different type or payload returns `409 Conflict`.

## Delivery API

```bash
curl -H "Authorization: Bearer $RELAYFORGE_API_KEY" \
  http://localhost:8080/v1/deliveries/<delivery-id>

curl -X POST \
  -H "Authorization: Bearer $RELAYFORGE_API_KEY" \
  http://localhost:8080/v1/deliveries/<delivery-id>/replay
```

Inspection returns the source event and the immutable attempt history. Only dead deliveries can be replayed. Replay preserves attempt numbering and grants a fresh endpoint-sized retry budget.

## Webhook protocol

RelayForge sends the event payload as a JSON `POST` request with these headers:

- `X-RelayForge-Delivery`
- `X-RelayForge-Event-ID`
- `X-RelayForge-Event`
- `X-RelayForge-Timestamp`
- `X-RelayForge-Signature`

The signature format is `v1=<hex HMAC-SHA256>`. The signed bytes are `<timestamp>.<delivery_id>.<raw_body>`. Redirects are never followed, environment proxies are ignored, and every resolved IP is checked before a connection is opened.

## Development

```bash
make test
make lint
make docker-config
```

Set `TEST_DATABASE_URL` and run `make test-integration` to execute the PostgreSQL queue suite and the end-to-end worker delivery test. Each integration test uses its own schema, so Go packages can run in parallel safely.

The application container runs as an unprivileged user with a read-only root filesystem, all Linux capabilities dropped, and `no-new-privileges` enabled. Only loopback ports are published by the development stack.

## License

MIT
