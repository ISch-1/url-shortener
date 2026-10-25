package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics implements service.Metrics and also exposes HTTP/rate-limit counters
// for the transport middlewares.
type Metrics struct {
	// HTTP
	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec

	// Cache
	CacheHits   prometheus.Counter
	CacheMisses prometheus.Counter

	// Singleflight
	SingleflightCalls  prometheus.Counter
	SingleflightShares prometheus.Counter

	// Domain
	LinksCreated     prometheus.Counter
	LinksDeactivated prometheus.Counter

	// Rate limit
	RateLimitAllowed  prometheus.Counter
	RateLimitRejected *prometheus.CounterVec
}

// NewMetrics registers every metric against the given registerer. Passing
// `prometheus.DefaultRegisterer` is the common case.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)
	return &Metrics{
		HTTPRequests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests, labelled by route and status class.",
		}, []string{"route", "status"}),
		HTTPDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		CacheHits:          factory.NewCounter(prometheus.CounterOpts{Name: "cache_hits_total"}),
		CacheMisses:        factory.NewCounter(prometheus.CounterOpts{Name: "cache_misses_total"}),
		SingleflightCalls:  factory.NewCounter(prometheus.CounterOpts{Name: "singleflight_calls_total"}),
		SingleflightShares: factory.NewCounter(prometheus.CounterOpts{Name: "singleflight_shared_total"}),
		LinksCreated:       factory.NewCounter(prometheus.CounterOpts{Name: "links_created_total"}),
		LinksDeactivated:   factory.NewCounter(prometheus.CounterOpts{Name: "links_deactivated_total"}),
		RateLimitAllowed:   factory.NewCounter(prometheus.CounterOpts{Name: "ratelimit_allowed_total"}),
		RateLimitRejected: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "ratelimit_rejected_total",
			Help: "Requests rejected by the rate limiter, labelled by reason.",
		}, []string{"reason"}),
	}
}

// Satisfy service.Metrics.
func (m *Metrics) CacheHit()         { m.CacheHits.Inc() }
func (m *Metrics) CacheMiss()        { m.CacheMisses.Inc() }
func (m *Metrics) SingleflightCall() { m.SingleflightCalls.Inc() }
func (m *Metrics) SingleflightShared() {
	m.SingleflightShares.Inc()
}
func (m *Metrics) LinkCreated()     { m.LinksCreated.Inc() }
func (m *Metrics) LinkDeactivated() { m.LinksDeactivated.Inc() }
