// Package middleware — HTTP middlewares.
package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ISch-1/url-shortener/internal/ratelimit"
	"github.com/ISch-1/url-shortener/internal/telemetry"
)

// RateLimitConfig is what the middleware needs to make a decision.
type RateLimitConfig struct {
	Limiter *ratelimit.TokenBucket
	Limits  ratelimit.Limits
	Metrics *telemetry.Metrics
	Log     *slog.Logger
}

// RateLimit is a middleware that admits at most `capacity` bursts and refills
// at `refill`/s per subject. Subject is the API key from `X-Api-Key` if
// present, otherwise the client IP.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := subjectFromRequest(r)

			decision, err := cfg.Limiter.Allow(r.Context(), subject, 1, cfg.Limits)
			if err != nil {
				// Fail-open — a broken Redis shouldn't take down the site.
				cfg.Log.WarnContext(r.Context(), "rate limiter error, admitting",
					"err", err, "subject", subject)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Limits.Capacity))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))

			if !decision.Allowed {
				cfg.Metrics.RateLimitRejected.WithLabelValues("over_limit").Inc()
				w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter/time.Second)+1))
				http.Error(w, `{"code":"rate_limited","message":"too many requests"}`, http.StatusTooManyRequests)
				return
			}
			cfg.Metrics.RateLimitAllowed.Inc()
			next.ServeHTTP(w, r)
		})
	}
}

func subjectFromRequest(r *http.Request) string {
	if key := r.Header.Get("X-Api-Key"); key != "" {
		return "k:" + key
	}
	// XFF first — respect what the LB set. Then RemoteAddr.
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := indexOf(fwd, ','); i >= 0 {
			return "ip:" + fwd[:i]
		}
		return "ip:" + fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "ip:" + host
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
