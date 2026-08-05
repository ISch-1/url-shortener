# 0001 — Snowflake ids + base62 codes

**Status:** Accepted
**Date:** 2026-07-13

## Context

Every new link needs a short, unique code. Three obvious options:

1. **Postgres `SERIAL` autoincrement.** One extra round-trip per create. Codes reveal the total number of links (competitive intel leak). Sharding later requires a coordinator.
2. **Random UUIDv4 → base62.** 22 characters. Long, ugly, and unnecessary — the ID space is way overkill for a shortener.
3. **Twitter-style Snowflake (timestamp + worker + sequence) → base62.** Locally generated (no DB round-trip), monotonically increasing (nice for BTree inserts), fits in `int64`, produces 7–11 character codes.

## Decision

Use a **Snowflake-inspired generator**:

- Bits 63..22 — millis since 2020-01-01 epoch (~139 years of headroom)
- Bits 21..12 — worker id (0..1023, set via `OB_WORKER_ID` env var)
- Bits 11..0  — 12-bit sequence within a millisecond (4096/ms per worker → 4M ids/sec/worker)

Then encode the `int64` with **base62** (`0-9A-Za-z`). A fresh id today is ~10 characters; deployments can widen to 11 or 12 without breaking existing links because the code is stored, not recomputed.

## Consequences

**Positive**
- Zero DB round-trips for id generation. Insert is a single `INSERT` with the id already known.
- Ordered on time. Postgres BTree loves this — no page splits from random inserts.
- Deterministic decoding — a code can be reversed to `(timestamp, worker, seq)` for debugging.
- Horizontal scaling: give each replica a unique `worker_id`, no coordination required.

**Negative**
- Codes reveal creation time to anyone who cares to decode them. For a URL shortener this is a non-issue (URLs are inherently public). For a system where opacity matters (invitation codes, password reset tokens), don't use this.
- Worker-id collision breaks uniqueness catastrophically. Ops responsibility to assign — either static config or a leased-id service like ZooKeeper. In V1 we require an env var and fail-fast on missing.
- The generator relies on the wall clock. Clock going backwards must be detected — we spin-wait for time to catch up rather than emit a possibly-duplicate id.

## Alternatives considered

1. **UUIDv7 (time-ordered UUID).** Same time-ordered benefit, but base62-encoded UUIDv7 is still 22 characters. Wrong length budget for a shortener.
2. **KGS (Key Generation Service) — pre-generate keys in a table, hand them out.** Adds an extra service and its own scaling concerns. Snowflake wins on operational simplicity.
3. **`nanoid`.** Random with configurable alphabet + length. Fine, but sacrifices the "monotonic + decodable" property that's actively useful for us.

## References

- [Twitter — Announcing Snowflake](https://blog.twitter.com/engineering/en_us/a/2010/announcing-snowflake) (2010)
- Alex Xu, *Grokking the System Design Interview* — URL shortener chapter
