# url-shortener

[![CI](https://github.com/ISch-1/url-shortener/actions/workflows/ci.yml/badge.svg)](https://github.com/ISch-1/url-shortener/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.23-00ADD8)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

> The classic "system design" project done properly, in Go. Distributed rate limiting via Redis Lua (one atomic round-trip), Snowflake ids + base62 codes (no DB round-trip on create), cache-aside on redirects with `singleflight` (no stampedes), REST **and** gRPC over one service, Prometheus + OTel out of the box, Testcontainers for integration tests, k6 for load. Runs from `docker compose up` + `go run`.

The point isn't "here's another URL shortener". The point is **every design decision has a tradeoff, an ADR, and a test that catches you when you get it wrong.** Rate-limit boundary effect? Token bucket. Cache stampede? Singleflight. Autoincrement leaks scale? Snowflake. Postgres broke in prod because H2 didn't catch it? Testcontainers.

---

## Endpoints

**REST — public API + redirects (`:8080`)**

| Verb | Path | Description |
|---|---|---|
| `POST`   | `/api/v1/links` | Create a short link |
| `GET`    | `/api/v1/links/{code}` | Fetch link info (no redirect) |
| `DELETE` | `/api/v1/links/{code}` | Deactivate a link |
| `GET`    | `/{code}` | **301** redirect to the original URL |

**gRPC — internal API (`:50051`)**

Same surface via `shortener.v1.ShortenerService` — `CreateLink`, `GetLink`, `ResolveCode`, `DeactivateLink`. Both transports call the same `service.Shortener` type; a bug fixed once is fixed for both.

**Metrics + health (`:9100`)** — `/metrics` (Prometheus) and `/health`.

---

## Quickstart

Prerequisites: Go 1.23+, Docker (Compose v2), `curl`, `jq`. For gRPC calls: `grpcurl`. For load tests: `k6`.

One-time on a fresh clone:

```bash
# Regenerate the .pb.go stubs from the proto file (requires protoc + Go plugins).
# The generated files land in pkg/api/v1 — the .gitignore lets you commit them
# if you'd rather not require protoc on every clone.
make proto
```

Then:

```bash
make up            # Postgres, Redis, Prometheus, Grafana, Jaeger, OTel Collector
make run           # start the binary — HTTP :8080, gRPC :50051, metrics :9100

# Create a link
curl -sf -X POST http://localhost:8080/api/v1/links \
  -H 'Content-Type: application/json' \
  -H 'X-Api-Key: demo-key' \
  -d '{"originalUrl":"https://example.com/hello"}' | jq

# Follow the redirect
curl -sI http://localhost:8080/{code} | grep -i location

# gRPC — resolve a code
grpcurl -plaintext -d '{"code":"{code}"}' localhost:50051 shortener.v1.ShortenerService/ResolveCode
```

Landmarks:

- HTTP API — <http://localhost:8080>
- Metrics — <http://localhost:9100/metrics>, Health — <http://localhost:9100/health>
- Prometheus — <http://localhost:9090>
- Grafana — <http://localhost:3000> → dashboard **URL Shortener — Overview**
- Jaeger — <http://localhost:16686>

---

## Design at a glance

- **[ADR 0001](docs/adr/0001-snowflake-plus-base62.md)** — Snowflake ids (worker-partitioned, 4M ids/s/worker) → base62 codes (~10 chars). No autoincrement, no DB round-trip on create.
- **[ADR 0002](docs/adr/0002-redis-lua-token-bucket.md)** — Distributed token bucket rate limiter in one atomic Redis Lua script. Correct across N replicas by construction.
- **[ADR 0003](docs/adr/0003-cache-aside-with-singleflight.md)** — Cache-aside on the hot redirect path, `golang.org/x/sync/singleflight` prevents Postgres stampede on hot codes.
- **[ADR 0004](docs/adr/0004-grpc-and-rest-side-by-side.md)** — REST + gRPC as thin transports over one service type.
- **[ADR 0005](docs/adr/0005-testcontainers-go-for-integration.md)** — Real Postgres + Redis in every integration test. Zero mocks for the DB or rate-limit Lua.

Full sequence diagrams (redirect hit, redirect miss with singleflight, rate-limit decision) live in [`docs/architecture.md`](docs/architecture.md).

---

## Tests

```bash
make test    # unit tests — no Docker, fast
make it      # integration tests — Testcontainers spins Postgres + Redis
```

The integration suite ([`test/integration/http_it_test.go`](test/integration/http_it_test.go)) exercises the real HTTP surface:

1. `POST /api/v1/links` → 201 with a `code`.
2. `GET /{code}` → **301** to the original URL. Verifies the round-trip end-to-end.
3. **Burst 10 requests with the same API key → at least one is 429** with `Retry-After` and `X-RateLimit-*` headers. This validates the Lua token bucket against a real Redis.

Unit suite highlights:
- `internal/idgen/snowflake_test.go` — monotonicity, concurrent uniqueness (8 goroutines × 5k ids, no duplicates), clock-regression handling.
- `internal/idgen/base62_test.go` — round-trip fuzz over 10 000 random `int64`s.

---

## Load test

```bash
make smoke     # k6: 500 VUs on redirects + 5 writes/s. Thresholds: p95 < 30 ms, err < 1%.
```

The script ([`load/create-and-redirect.js`](load/create-and-redirect.js)) seeds 50 codes at startup, then runs two scenarios in parallel — a **readers** ramp on the redirect path and a **writers** constant-rate scenario on the create path (which is rate-limited, so it exercises both the 201 and the 429).

Watch the Grafana dashboard live during the run: request rate by route, p95/p99 latency, cache hit ratio, rate-limit allowed vs rejected, singleflight coalesce ratio.

---

## Repo layout

```
url-shortener/
├── go.mod
├── Dockerfile               # multi-stage → distroless/static:nonroot
├── docker-compose.yml       # Postgres, Redis, Prometheus, Grafana, Jaeger, OTel
├── Makefile
│
├── api/proto/shortener/v1/  # protobuf source of truth
│   └── shortener.proto
├── pkg/api/v1/              # generated .pb.go (run `make proto`)
│
├── cmd/shortener/main.go    # binary entry — wires everything + graceful shutdown
│
├── internal/
│   ├── config/              # envconfig-driven
│   ├── domain/              # Link, sentinel errors
│   ├── idgen/               # Snowflake + base62 (fully unit-tested)
│   ├── storage/postgres/    # pgx pool + embedded migrations + Store methods
│   │   └── migrations/0001_links.sql
│   ├── cache/               # Redis LinkCache
│   ├── ratelimit/
│   │   ├── tokenbucket.go   # Go wrapper
│   │   └── lua/token_bucket.lua   # atomic bucket, documented arg-by-arg
│   ├── service/             # Shortener — singleflight, validation, orchestration
│   ├── http/
│   │   ├── handlers.go      # REST layer
│   │   ├── server.go        # Mux composition + OTel wrapper
│   │   └── middleware/      # rate-limit + observability
│   ├── grpc/                # gRPC server — thin wrapper over service.Shortener
│   └── telemetry/
│       ├── logger.go        # slog + trace-id injection
│       ├── tracing.go       # OTLP gRPC exporter
│       └── metrics.go       # Prometheus counters/histograms
│
├── test/integration/        # `//go:build integration` — real Postgres + Redis
│
├── load/create-and-redirect.js
│
├── docs/
│   ├── architecture.md
│   └── adr/                 # 5 ADRs
│
├── infra/                   # docker-compose service configs
│   ├── postgres/init.sql
│   ├── prometheus/prometheus.yml
│   ├── grafana/             # provisioned datasources + dashboard JSON
│   └── otel/otel-collector-config.yaml
│
└── .github/workflows/ci.yml # fmt · vet · staticcheck · unit · integration · image
```

---

## Roadmap

- [x] REST + gRPC + rate limit + cache-aside + singleflight
- [x] Snowflake + base62 with unit tests
- [x] Testcontainers-go integration suite
- [x] Prometheus + Grafana + OTel + Jaeger
- [x] k6 baseline
- [ ] **Kubernetes manifests** — Deployment + Service + HPA (CPU + custom metric on `ratelimit_rejected_total`)
- [ ] **NGINX / Envoy** in front for TLS termination + connection multiplexing
- [ ] **Pub/sub cache invalidation** across replicas (goodbye "up to CACHE_TTL stale after DELETE")
- [ ] **QR-code endpoint** — same input, PNG output, cached
- [ ] **Bulk create** endpoint with a stream RPC on the gRPC side
- [ ] **Failed-lookup negative cache** (short TTL) to blunt attacks that scan for valid codes

---

## Why this project exists

Alongside [ecommerce-polyglot](https://github.com/ISch-1/ecommerce-polyglot) (Java · polyglot persistence), [event-stream-platform](https://github.com/ISch-1/event-stream-platform) (Java · Kafka · ClickHouse), and [order-book](https://github.com/ISch-1/order-book) (Rust · low-latency systems), this repo covers the "system design classic" leg — the exact shape of the question every senior interview asks. Go, real Postgres, real Redis, real observability, and enough restraint to keep it readable.

## License

MIT — see [LICENSE](LICENSE).
