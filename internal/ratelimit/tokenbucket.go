// Package ratelimit is a distributed token-bucket limiter backed by Redis.
//
// The bucket lives in Redis; the decision is computed atomically in a Lua
// script (see lua/token_bucket.lua). One round-trip per request.
package ratelimit

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/token_bucket.lua
var tokenBucketLuaSrc string

// Decision is the outcome of an admission check.
type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limits describes a bucket's shape. Zero refill or capacity means "no limiting"
// (Allow always returns Allowed=true).
type Limits struct {
	Capacity          int     // max burst
	RefillPerSecond   float64 // steady-state throughput
}

type TokenBucket struct {
	client *redis.Client
	script *redis.Script
	ttl    time.Duration
}

func New(client *redis.Client, ttl time.Duration) *TokenBucket {
	return &TokenBucket{
		client: client,
		script: redis.NewScript(tokenBucketLuaSrc),
		ttl:    ttl,
	}
}

// Allow charges `cost` tokens against the bucket keyed by `subject` under
// the given limits. Returns the decision. Errors are Redis I/O only; the
// caller should decide whether to fail-open or fail-closed.
func (tb *TokenBucket) Allow(ctx context.Context, subject string, cost int, l Limits) (Decision, error) {
	if l.Capacity == 0 || l.RefillPerSecond == 0 {
		return Decision{Allowed: true, Remaining: -1}, nil
	}
	key := "rl:" + subject
	now := time.Now().UnixMilli()

	res, err := tb.script.Run(ctx, tb.client, []string{key},
		l.Capacity, l.RefillPerSecond, now, cost, tb.ttl.Milliseconds()).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit script: %w", err)
	}

	arr, ok := res.([]any)
	if !ok || len(arr) != 3 {
		return Decision{}, errors.New("ratelimit script returned unexpected shape")
	}
	allowed, _ := arr[0].(int64)
	remaining, _ := arr[1].(int64)
	retryMs, _ := arr[2].(int64)

	return Decision{
		Allowed:    allowed == 1,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryMs) * time.Millisecond,
	}, nil
}
