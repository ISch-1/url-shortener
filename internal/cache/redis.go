// Package cache wraps go-redis with a typed API for link snapshots.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ISch-1/url-shortener/internal/domain"
)

type LinkCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewLink(client *redis.Client, ttl time.Duration) *LinkCache {
	return &LinkCache{client: client, ttl: ttl}
}

func (c *LinkCache) key(code string) string { return "link:" + code }

// Get returns the cached link, (nil, nil) on cache miss, or an error.
func (c *LinkCache) Get(ctx context.Context, code string) (*domain.Link, error) {
	raw, err := c.client.Get(ctx, c.key(code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l domain.Link
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (c *LinkCache) Set(ctx context.Context, l domain.Link) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(l.Code), b, c.ttl).Err()
}

func (c *LinkCache) Delete(ctx context.Context, code string) error {
	return c.client.Del(ctx, c.key(code)).Err()
}
