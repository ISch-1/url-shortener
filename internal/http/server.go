package http

import (
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ISch-1/url-shortener/internal/http/middleware"
	"github.com/ISch-1/url-shortener/internal/ratelimit"
	"github.com/ISch-1/url-shortener/internal/telemetry"
)

type MuxDeps struct {
	Handlers *Handlers
	Limiter  *ratelimit.TokenBucket
	Limits   ratelimit.Limits
	Metrics  *telemetry.Metrics
	Log      *slog.Logger
}

// NewMux composes the HTTP router — Go 1.22 ServeMux with method + path
// patterns. The rate-limit middleware is applied only to the API routes
// (not the redirect path, which needs to be as fast as possible).
func NewMux(d MuxDeps) http.Handler {
	rl := middleware.RateLimit(middleware.RateLimitConfig{
		Limiter: d.Limiter,
		Limits:  d.Limits,
		Metrics: d.Metrics,
		Log:     d.Log,
	})
	obs := middleware.Observability(d.Metrics, d.Log)

	mux := http.NewServeMux()

	// Public API — rate-limited.
	mux.Handle("POST /api/v1/links", obs(rl(http.HandlerFunc(d.Handlers.Create))))
	mux.Handle("GET /api/v1/links/{code}", obs(rl(http.HandlerFunc(d.Handlers.Get))))
	mux.Handle("DELETE /api/v1/links/{code}", obs(rl(http.HandlerFunc(d.Handlers.Delete))))

	// Redirect — hot path, no rate limit middleware, still observed.
	mux.Handle("GET /{code}", obs(http.HandlerFunc(d.Handlers.Redirect)))

	// Wrap the whole thing in OTel HTTP instrumentation so every request
	// becomes a root span with the proper W3C-standard attributes.
	return otelhttp.NewHandler(mux, "http.server")
}
