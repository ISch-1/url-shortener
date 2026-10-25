// Package config exposes the app configuration. Loaded from env with defaults
// suitable for local dev; every field is overridable in a container manifest.
package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// Networking
	HTTPAddr    string `envconfig:"HTTP_ADDR"    default:"0.0.0.0:8080"`
	GRPCAddr    string `envconfig:"GRPC_ADDR"    default:"0.0.0.0:50051"`
	MetricsAddr string `envconfig:"METRICS_ADDR" default:"0.0.0.0:9100"`

	// Snowflake worker — must be unique per replica.
	WorkerID int64 `envconfig:"WORKER_ID" default:"1"`

	// Postgres
	PostgresURL            string        `envconfig:"POSTGRES_URL"      default:"postgres://shortener:shortener@localhost:5432/shortener?sslmode=disable"`
	PostgresMaxConns       int32         `envconfig:"POSTGRES_MAX_CONNS" default:"20"`
	PostgresConnectTimeout time.Duration `envconfig:"POSTGRES_CONNECT_TIMEOUT" default:"3s"`

	// Redis
	RedisAddr     string `envconfig:"REDIS_ADDR"      default:"localhost:6379"`
	RedisPoolSize int    `envconfig:"REDIS_POOL_SIZE" default:"64"`

	// Cache
	CacheTTL time.Duration `envconfig:"CACHE_TTL" default:"1h"`

	// Rate limiter defaults (per-subject: API key, IP)
	RateLimitCapacity        int     `envconfig:"RATE_LIMIT_CAPACITY" default:"120"`
	RateLimitRefillPerSecond float64 `envconfig:"RATE_LIMIT_REFILL_PER_SECOND" default:"2.0"`

	// Public base URL for building short links (e.g. https://sho.rt)
	PublicBaseURL string `envconfig:"PUBLIC_BASE_URL" default:"http://localhost:8080"`

	// Observability
	OTLPEndpoint string `envconfig:"OTLP_ENDPOINT" default:"localhost:4317"`
	LogLevel     string `envconfig:"LOG_LEVEL"     default:"info"`
	LogFormat    string `envconfig:"LOG_FORMAT"    default:"text"` // "text" or "json"

	// Graceful shutdown
	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"15s"`
}

func Load() (Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return Config{}, fmt.Errorf("load env: %w", err)
	}
	if c.WorkerID <= 0 || c.WorkerID > 1023 {
		return Config{}, fmt.Errorf("WORKER_ID must be in 1..1023, got %d", c.WorkerID)
	}
	return c, nil
}
