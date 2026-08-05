# 0003 — Cache-aside + singleflight on redirect

**Status:** Accepted
**Date:** 2026-07-13

## Context

The redirect path (`GET /:code` → 301) dominates traffic in every URL shortener. Cache hits should take microseconds; misses shouldn't stampede Postgres.

## Decision

Cache-aside with a **process-local singleflight**:

1. Look up the code in Redis. Hit → return in one round-trip.
2. Miss → the loader wraps the Postgres query with `golang.org/x/sync/singleflight`. Concurrent requests for the same code all wait on the same in-flight loader; only one query hits Postgres.
3. On success, backfill Redis with a 1 h TTL (configurable).
4. On write path (`DELETE`), `DEL` the key locally. Multi-replica invalidation happens on TTL expiry (~1 h worst case).

Redis is configured with `maxmemory-policy allkeys-lru` — the shortener has no notion of "important" links, so LRU eviction is the right default.

## Consequences

**Positive**
- Cache is a strict acceleration layer. Losing Redis entirely just slows the site down to Postgres speed; no correctness change.
- Singleflight bounds Postgres load under a hot-code stampede — a single loader per code per process, no matter how many concurrent redirects hit.
- No cache-write path — all mutations go through Postgres first, so Redis can never disagree with the source of truth for longer than one TTL.

**Negative**
- Delete propagates in up to one TTL. For a URL shortener, acceptable. For a system where a deleted resource must be gone *now*, add pub/sub on `DEL` events and subscribe in every replica.
- The singleflight coalesces per-process. A ten-replica deployment can hit Postgres ten times on a hot code, not once. Acceptable trade-off — cross-process singleflight requires distributed locking which for this workload isn't worth it.

## Alternatives considered

1. **No cache.** Postgres reads at 10k RPS are a bad idea if Redis at 100k+ RPS is one line of config away.
2. **Write-through.** Every write updates Postgres and Redis atomically. Not atomic — two systems, no XA. Same class of bug as dual-write outboxes solve on the write path.
3. **Redis pub/sub for invalidations.** Correct and cheap; on the roadmap. V1 keeps it out of the critical path.

## References

- [singleflight docs](https://pkg.go.dev/golang.org/x/sync/singleflight)
- Discord — [scaling to millions of users on cache-aside](https://discord.com/blog/how-discord-stores-billions-of-messages)
