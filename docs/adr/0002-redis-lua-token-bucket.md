# 0002 — Distributed rate limiting via Redis Lua

**Status:** Accepted
**Date:** 2026-07-13

## Context

A stateless HTTP service running behind a load balancer cannot rate-limit in-process — every replica would see only its share of a caller's traffic. The rate limiter has to be **shared state**.

The classic patterns:

1. **Fixed window** in Redis (`INCR` + `EXPIRE`). Cheap, but suffers the boundary effect: 2× the limit can pass in the two seconds around a window rollover.
2. **Sliding-window log**. Correct, but O(n) memory per key where n = requests in the window. Rejected at scale.
3. **Token bucket** with two fields (tokens remaining, last refill time). Corrects the boundary effect, O(1) per request, natural burst allowance.

Doing token bucket with two round-trips (`GET`, `SET`) races: two replicas can both see "8 tokens left", both decrement to 7, both admit the request. That's the classic dual-write race.

## Decision

Token bucket in **one atomic Redis `EVALSHA`**. A Lua script (`internal/ratelimit/lua/token_bucket.lua`) reads the current state, computes new tokens after refill, decides admit/reject, writes the new state, and returns the decision — all inside Redis's single-threaded execution model. No race.

- Config: `capacity` (max burst), `refill_rate` (tokens/second).
- One key per (limiter_id, subject), where subject is API key or client IP.
- Script loaded once at startup, referenced by SHA1 thereafter (saves the round-trip).

Response headers include `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `Retry-After` when rejected.

## Consequences

**Positive**
- Correct under concurrency across any number of replicas — Redis serialises Lua execution per key.
- One round-trip per request. p99 measured under 1 ms at 5k RPS locally.
- Bucket state is just two fields (`tokens`, `updated_ms`) — trivial memory footprint.

**Negative**
- Redis is now on the critical path for every request. Mitigations: Redis pool with generous connection limits, fail-open on Redis error (log + admit — degrade to unlimited rather than deny everyone). ADR entry when tightening this to fail-closed.
- Global rate limiter — same replica-shared state. A regional deploy would need per-region Redis. Documented; irrelevant for V1.

## Alternatives considered

1. **`INCR` + `EXPIRE`.** See context — boundary effect.
2. **Local (in-process) bucket per replica.** Simpler, wrong. Under 3 replicas an attacker sends 3× the configured rate before being throttled.
3. **Envoy / NGINX built-in.** Fine for gross rate-limiting; not per-API-key with dynamic config, which the service actually needs.

## References

- Kleppmann, *DDIA*, ch. 9 on consensus / atomicity guarantees
- [Redis EVAL / EVALSHA docs](https://redis.io/docs/latest/commands/eval/)
- Stripe engineering — [scaling rate limiters](https://stripe.com/blog/rate-limiters)
