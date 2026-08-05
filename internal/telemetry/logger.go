// Package telemetry wires slog + Prometheus + OTel. Everything the transport
// layers need is exposed as a small typed API.
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger builds a slog.Logger honouring level ("debug"|"info"|"warn"|"error")
// and format ("json"|"text").
func NewLogger(level, format string, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if strings.EqualFold(format, "json") {
		h = slog.NewJSONHandler(out, opts)
	} else {
		h = slog.NewTextHandler(out, opts)
	}
	return slog.New(&traceContextHandler{inner: h})
}

// traceContextHandler enriches every log record with trace/span IDs when the
// context carries an active OTel span. Not a public type — it's just glue.
type traceContextHandler struct{ inner slog.Handler }

func (h *traceContextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *traceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if sc := span.SpanContext(); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *traceContextHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &traceContextHandler{inner: h.inner.WithAttrs(as)}
}

func (h *traceContextHandler) WithGroup(g string) slog.Handler {
	return &traceContextHandler{inner: h.inner.WithGroup(g)}
}
