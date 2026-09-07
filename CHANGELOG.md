# Changelog

## v0.1.0 - 2026-09-07

- Added idempotent event ingestion and encrypted webhook endpoints.
- Added a PostgreSQL queue with fenced leases, retries, dead-letter state, replay, and immutable attempt history.
- Added signed webhook delivery with DNS pinning, SSRF protection, response limits, and endpoint circuit breakers.
- Added Prometheus metrics, structured logs, health checks, and build information.
- Added a hardened Docker Compose environment with versioned migrations.
- Added race-tested PostgreSQL integration coverage, API contract linting, vulnerability scanning, and image builds in CI.
- Added a concurrent ingestion load runner with latency percentiles.
