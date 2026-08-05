# 0005 — Testcontainers-go for integration tests

**Status:** Accepted
**Date:** 2026-07-13

## Context

Integration tests need real Postgres and real Redis. The alternatives (in-memory Postgres approximations, mock Redis clients) diverge from production in subtle ways — LSN semantics, `ILIKE` case folding, Lua atomicity, keyspace notifications. Every one of those has bitten someone I know in the last two years.

## Decision

Every `*_it_test.go` / test under `test/integration/` uses **`testcontainers-go`** with the singleton container pattern: containers start once per test binary, are shared across every test in the package, and get reaped on process exit.

- `postgres` module for Postgres.
- `redis` module for Redis.
- Ryuk (Testcontainers' cleanup daemon) disabled in CI (`TESTCONTAINERS_RYUK_DISABLED=true`) because runners are ephemeral; enabled locally.
- Tests use a Go build tag `//go:build integration` so `go test ./...` (unit) is fast and Docker-free; `go test -tags=integration ./test/integration/...` runs the heavy suite.

## Consequences

**Positive**
- Tests catch bugs in the *actual* Postgres SQL dialect and *actual* Redis Lua semantics. The rate-limiter Lua script has been changed twice — both times the test caught the bug immediately.
- The dev loop is fast for the common case (unit tests) but a full pre-push run against real deps is one command away.
- CI on GitHub Actions runs integration tests reliably because runners come with Docker.

**Negative**
- ~10 s of container startup per test binary. Amortised across tests in the same package.
- Requires Docker on developer laptops. Documented in the README.

## Alternatives considered

1. **`sqlmock` + `redismock`.** Great for testing the shape of queries; useless for testing what Postgres/Redis actually do. False confidence.
2. **Long-lived dev Postgres + shared Redis.** Cross-engineer flakiness and no parallel CI.
3. **Embedded Postgres via `zonkyio/embedded-postgres-binaries`.** Real Postgres, no Docker. Considered — but Docker is already required for local dev anyway, and Testcontainers is the more portable pattern for someone reading the code.
