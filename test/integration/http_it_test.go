//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/ISch-1/url-shortener/internal/cache"
	apphttp "github.com/ISch-1/url-shortener/internal/http"
	"github.com/ISch-1/url-shortener/internal/idgen"
	"github.com/ISch-1/url-shortener/internal/ratelimit"
	"github.com/ISch-1/url-shortener/internal/service"
	"github.com/ISch-1/url-shortener/internal/storage/postgres"
	"github.com/ISch-1/url-shortener/internal/telemetry"
)

// harness is a fully-wired app-in-a-process with real Postgres + Redis behind it.
type harness struct {
	server *httptest.Server
	svc    *service.Shortener
	stop   func()
}

var (
	sharedOnce sync.Once
	shared     *harness
	sharedErr  error
)

func startHarness(t *testing.T) *harness {
	t.Helper()
	sharedOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		pg, err := tcpg.Run(ctx, "postgres:16-alpine",
			tcpg.WithUsername("shortener"),
			tcpg.WithPassword("shortener"),
			tcpg.WithDatabase("shortener"),
			// The image restarts Postgres once after initdb; without this, the
			// first connection can land mid-restart and fail with an EOF.
			tcpg.BasicWaitStrategies(),
			tc.WithLogger(tc.TestLogger(t)),
		)
		if err != nil {
			sharedErr = err
			return
		}
		pgDSN, err := pg.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			sharedErr = err
			return
		}

		rd, err := tcredis.Run(ctx, "redis:7-alpine")
		if err != nil {
			sharedErr = err
			return
		}
		rdAddr, err := rd.ConnectionString(ctx)
		if err != nil {
			sharedErr = err
			return
		}

		store, err := postgres.New(ctx, pgDSN, 10, 3*time.Second)
		if err != nil {
			sharedErr = err
			return
		}
		rdb := redis.NewClient(&redis.Options{
			Addr: rdAddr[len("redis://"):],
		})
		linkCache := cache.NewLink(rdb, 1*time.Minute)
		limiter := ratelimit.New(rdb, 10*time.Minute)
		gen, _ := idgen.New(1)
		reg := prometheus.NewRegistry()
		metrics := telemetry.NewMetrics(reg)
		log := telemetry.NewLogger("warn", "text", io.Discard)

		svc := service.New(store, linkCache, gen, metrics)
		handlers := apphttp.NewHandlers(svc, "http://sho.rt", log)
		mux := apphttp.NewMux(apphttp.MuxDeps{
			Handlers: handlers,
			Limiter:  limiter,
			Limits:   ratelimit.Limits{Capacity: 5, RefillPerSecond: 0.1},
			Metrics:  metrics,
			Log:      log,
		})
		srv := httptest.NewServer(mux)

		shared = &harness{
			server: srv,
			svc:    svc,
			stop: func() {
				srv.Close()
				store.Close()
				_ = rdb.Close()
				_ = pg.Terminate(context.Background())
				_ = rd.Terminate(context.Background())
			},
		}
	})
	require.NoError(t, sharedErr)
	return shared
}

func TestHTTP_CreateThenRedirect(t *testing.T) {
	h := startHarness(t)

	// POST /api/v1/links
	body := []byte(`{"originalUrl":"https://example.com/hello"}`)
	req, _ := stdhttp.NewRequest("POST", h.server.URL+"/api/v1/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key-create")
	res, err := stdhttp.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, stdhttp.StatusCreated, res.StatusCode)

	var out struct {
		Code     string `json:"code"`
		ShortURL string `json:"shortUrl"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	res.Body.Close()
	require.NotEmpty(t, out.Code)

	// GET /{code} — redirect to originalUrl (don't follow).
	client := &stdhttp.Client{
		CheckRedirect: func(req *stdhttp.Request, via []*stdhttp.Request) error {
			return stdhttp.ErrUseLastResponse
		},
	}
	res, err = client.Get(h.server.URL + "/" + out.Code)
	require.NoError(t, err)
	assert.Equal(t, stdhttp.StatusMovedPermanently, res.StatusCode)
	assert.Equal(t, "https://example.com/hello", res.Header.Get("Location"))
	res.Body.Close()
}

func TestHTTP_RateLimiterRejectsBurst(t *testing.T) {
	h := startHarness(t)

	// Capacity is 5 in the harness. The 6th create in a burst must be 429.
	client := stdhttp.DefaultClient
	var lastStatus int
	for i := 0; i < 10; i++ {
		body := []byte(`{"originalUrl":"https://example.com/burst"}`)
		req, _ := stdhttp.NewRequest("POST", h.server.URL+"/api/v1/links", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", "burst-subject")
		res, err := client.Do(req)
		require.NoError(t, err)
		res.Body.Close()
		lastStatus = res.StatusCode
		if res.StatusCode == stdhttp.StatusTooManyRequests {
			assert.NotEmpty(t, res.Header.Get("Retry-After"))
			assert.NotEmpty(t, res.Header.Get("X-RateLimit-Limit"))
			return
		}
	}
	t.Fatalf("expected a 429 within 10 requests; last status was %d", lastStatus)
}

func TestHTTP_UnknownCodeReturns404(t *testing.T) {
	h := startHarness(t)

	res, err := stdhttp.Get(h.server.URL + "/api/v1/links/does-not-exist")
	require.NoError(t, err)
	assert.Equal(t, stdhttp.StatusNotFound, res.StatusCode)
	res.Body.Close()
}
