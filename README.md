# RelayForge

RelayForge is a reliable webhook delivery service written in Go. It accepts events once, delivers them asynchronously, and makes failed deliveries observable and recoverable.

The project is built as a production-oriented portfolio service rather than a framework demo. The implementation is developed in small, verified increments.

## Planned capabilities

- idempotent event ingestion
- durable PostgreSQL-backed delivery queue
- concurrent workers with leases and `SKIP LOCKED`
- exponential backoff with jitter and dead-letter handling
- HMAC request signing
- encrypted endpoint secrets
- SSRF-resistant outbound requests
- circuit breaking and delivery rate limits
- Prometheus metrics and structured logs
- Docker Compose development environment

## Current state

The service exposes liveness and database-backed readiness endpoints, manages a bounded PostgreSQL connection pool, and supports graceful shutdown. The durable queue uses `FOR UPDATE SKIP LOCKED`, expiring leases, per-claim fencing tokens, and immutable delivery attempts.

```bash
make run
curl http://localhost:8080/health/live
```

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

## License

MIT
