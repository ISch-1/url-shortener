# Architecture

## C4 — Level 1: System Context

```mermaid
graph LR
    users[HTTP clients<br/>browsers · curl · third parties]
    internal[Internal services<br/>gRPC clients]
    ops[Grafana]
    us[url-shortener<br/>Go 1.23]
    jaeger[(Jaeger)]

    users -->|HTTP :8080| us
    internal -->|gRPC :50051| us
    us -->|OTLP| jaeger
    ops -.->|scrape /metrics :9100| us
```

## C4 — Level 2: Containers (process-internal)

```mermaid
graph TB
    subgraph transport
        http[HTTP handlers<br/>net/http + Go 1.22 ServeMux]
        grpc[gRPC server<br/>tonic-generated stubs]
    end

    subgraph middleware
        obs[Observability<br/>metrics + slog + OTel]
        rl[Rate limit<br/>Redis Lua token bucket]
    end

    svc[Shortener service<br/>singleflight + validation]

    subgraph deps
        cache[(Redis<br/>link snapshots)]
        redisrl[(Redis<br/>rate-limit state)]
        pg[(Postgres<br/>links source of truth)]
        gen[Snowflake generator]
    end

    http --> obs --> rl --> svc
    grpc --> svc
    svc --> gen
    svc --> cache
    svc --> pg
    rl --> redisrl
```

## Redirect flow (cache HIT — the fast path)

```mermaid
sequenceDiagram
    autonumber
    participant client as HTTP client
    participant obs as Observability MW
    participant h as Redirect handler
    participant svc as Shortener
    participant redis as Redis

    client->>obs: GET /{code}
    obs->>h: forward
    h->>svc: Resolve(code)
    svc->>redis: GET link:{code}
    redis-->>svc: cached snapshot
    svc-->>h: (url, ok=true)
    h-->>client: 301 → originalUrl
    Note over h: fire-and-forget goroutine bumps click_count in Postgres
```

## Redirect flow (cache MISS — singleflight coalesces loaders)

```mermaid
sequenceDiagram
    autonumber
    participant a as request A
    participant b as request B
    participant svc as Shortener
    participant sf as singleflight
    participant redis as Redis
    participant pg as Postgres

    par
        a->>svc: Resolve(code)
        svc->>redis: GET → miss
        svc->>sf: Do("code", loader)
    and
        b->>svc: Resolve(code)
        svc->>redis: GET → miss
        svc->>sf: Do("code", loader)
    end

    Note over sf: only the first caller runs the loader<br/>the second waits on the same result

    sf->>pg: SELECT ...
    pg-->>sf: row
    sf->>redis: SET link:{code}
    sf-->>a: link
    sf-->>b: link
```

## Rate limit flow

```mermaid
sequenceDiagram
    autonumber
    participant client as HTTP client
    participant mw as Rate-limit MW
    participant redis as Redis

    client->>mw: POST /api/v1/links (X-Api-Key: k)
    mw->>redis: EVALSHA token_bucket.lua<br/>KEYS=[rl:k:k] ARGV=[cap, refill, now_ms, cost, ttl_ms]
    Note over redis: Lua runs atomically:<br/>refill → decide → write → return
    redis-->>mw: {allowed, remaining, retry_after_ms}

    alt allowed
        mw->>mw: X-RateLimit-Limit, X-RateLimit-Remaining
        mw-->>client: 201 / 200
    else denied
        mw->>mw: Retry-After
        mw-->>client: 429
    end
```

## Concurrency + backpressure

- **Snowflake generator** is guarded by a single mutex — cheap because operations are ~50 ns each.
- **Postgres pool** is bounded via `pgxpool` (`POSTGRES_MAX_CONNS`, default 20). Requests block on `Acquire` when the pool is exhausted — a natural backpressure signal.
- **Redis pool** is bounded (`REDIS_POOL_SIZE`, default 64). Both cache reads and rate-limit checks share it.
- **`ReadHeaderTimeout`** on both HTTP servers prevents slowloris.
- **Graceful shutdown** on `SIGTERM` / `Ctrl+C`: HTTP `Shutdown()`, gRPC `GracefulStop()`, tracing flush, with a bounded deadline (`SHUTDOWN_TIMEOUT`, default 15 s).

## Consistency model

- **Read** — cache-aside: cache never contains a link that Postgres doesn't. It may contain a stale copy for up to `CACHE_TTL` after a `DELETE`. Acceptable for a URL shortener; if not, pub/sub invalidation is one file away (roadmap).
- **Write** — Postgres is source of truth. Creation returns after the row is committed; cache backfill happens on the first read.
- **Rate limit** — Redis is authoritative. Cross-replica consistent by construction because Lua serialises per-key.
