-- Token bucket rate limiter, atomic in one Redis round-trip.
--
-- KEYS[1]  bucket state key (a Redis hash with fields: tokens, updated_ms)
-- ARGV[1]  capacity            (max burst — integer)
-- ARGV[2]  refill_rate         (tokens per second — float)
-- ARGV[3]  now_ms              (client-supplied wall clock in millis)
-- ARGV[4]  cost                (tokens this request consumes — integer)
-- ARGV[5]  ttl_ms              (key TTL to garbage-collect idle buckets)
--
-- Returns a table {allowed:int, remaining:int, retry_after_ms:int}.
--   allowed         1 if the request may proceed, 0 otherwise
--   remaining       tokens remaining after the decision
--   retry_after_ms  millis the caller should wait before retrying (0 when allowed)

local key         = KEYS[1]
local capacity    = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now_ms      = tonumber(ARGV[3])
local cost        = tonumber(ARGV[4])
local ttl_ms      = tonumber(ARGV[5])

local state = redis.call('HMGET', key, 'tokens', 'updated_ms')
local tokens = tonumber(state[1])
local updated = tonumber(state[2])

if tokens == nil then
    tokens = capacity
    updated = now_ms
end

-- Refill based on elapsed time. Cap at capacity.
local elapsed_s = math.max(0, (now_ms - updated) / 1000.0)
tokens = math.min(capacity, tokens + elapsed_s * refill_rate)

local allowed = 0
local retry_after_ms = 0
if tokens >= cost then
    tokens = tokens - cost
    allowed = 1
else
    local missing = cost - tokens
    retry_after_ms = math.ceil((missing / refill_rate) * 1000)
end

redis.call('HMSET', key, 'tokens', tokens, 'updated_ms', now_ms)
redis.call('PEXPIRE', key, ttl_ms)

return { allowed, math.floor(tokens), retry_after_ms }
