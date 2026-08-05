package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ISch-1/url-shortener/internal/telemetry"
)

// Observability wraps a handler in metrics + a structured access log line.
// The `route` label comes from the ServeMux pattern via r.Pattern (Go 1.22+).
func Observability(m *telemetry.Metrics, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := r.Pattern
			if route == "" {
				route = "unknown"
			}
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(rw, r)

			dur := time.Since(start)
			m.HTTPRequests.WithLabelValues(route, statusClass(rw.status)).Inc()
			m.HTTPDuration.WithLabelValues(route).Observe(dur.Seconds())

			log.InfoContext(r.Context(), "http",
				"method", r.Method,
				"path", r.URL.Path,
				"route", route,
				"status", rw.status,
				"duration_ms", dur.Milliseconds(),
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rw *statusRecorder) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func statusClass(code int) string {
	return strconv.Itoa(code/100) + "xx"
}
