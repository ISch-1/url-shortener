// Binary entry point. Wires config → dependencies → service → HTTP/gRPC/metrics
// servers, and orchestrates graceful shutdown.
package main

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	pb "github.com/ISch-1/url-shortener/pkg/api/v1"

	"github.com/ISch-1/url-shortener/internal/cache"
	"github.com/ISch-1/url-shortener/internal/config"
	appgrpc "github.com/ISch-1/url-shortener/internal/grpc"
	apphttp "github.com/ISch-1/url-shortener/internal/http"
	"github.com/ISch-1/url-shortener/internal/idgen"
	"github.com/ISch-1/url-shortener/internal/ratelimit"
	"github.com/ISch-1/url-shortener/internal/service"
	"github.com/ISch-1/url-shortener/internal/storage/postgres"
	"github.com/ISch-1/url-shortener/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := telemetry.NewLogger(cfg.LogLevel, cfg.LogFormat, os.Stdout)
	log.Info("starting", "grpc_addr", cfg.GRPCAddr, "http_addr", cfg.HTTPAddr, "metrics_addr", cfg.MetricsAddr, "worker_id", cfg.WorkerID)

	startCtx, cancelStart := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStart()

	// ─── observability ─────────────────────────────────────────────
	shutdownTracing, err := telemetry.InitTracing(startCtx, "url-shortener", cfg.OTLPEndpoint)
	if err != nil {
		log.Warn("tracing not available — continuing without it", "err", err)
	}
	metrics := telemetry.NewMetrics(prometheus.DefaultRegisterer)

	// ─── dependencies ──────────────────────────────────────────────
	store, err := postgres.New(startCtx, cfg.PostgresURL, cfg.PostgresMaxConns, cfg.PostgresConnectTimeout)
	if err != nil {
		return err
	}
	defer store.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		PoolSize: cfg.RedisPoolSize,
	})
	defer rdb.Close()

	if err := rdb.Ping(startCtx).Err(); err != nil {
		return err
	}

	linkCache := cache.NewLink(rdb, cfg.CacheTTL)
	limiter := ratelimit.New(rdb, 10*time.Minute)

	gen, err := idgen.New(cfg.WorkerID)
	if err != nil {
		return err
	}

	svc := service.New(store, linkCache, gen, metrics)

	// ─── HTTP server (public API + redirects) ──────────────────────
	handlers := apphttp.NewHandlers(svc, cfg.PublicBaseURL, log)
	httpMux := apphttp.NewMux(apphttp.MuxDeps{
		Handlers: handlers,
		Limiter:  limiter,
		Limits:   ratelimit.Limits{Capacity: cfg.RateLimitCapacity, RefillPerSecond: cfg.RateLimitRefillPerSecond},
		Metrics:  metrics,
		Log:      log,
	})
	httpSrv := &stdhttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ─── HTTP server for /metrics + /health ────────────────────────
	metricsMux := stdhttp.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsMux.HandleFunc("/health", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	metricsSrv := &stdhttp.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ─── gRPC server ───────────────────────────────────────────────
	grpcSrv := grpc.NewServer()
	pb.RegisterShortenerServiceServer(grpcSrv, appgrpc.New(svc))
	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	// ─── run all three ─────────────────────────────────────────────
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 3)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	go func() { errCh <- metricsSrv.ListenAndServe() }()
	go func() { errCh <- grpcSrv.Serve(grpcListener) }()

	log.Info("listening")

	select {
	case err := <-errCh:
		if !errors.Is(err, stdhttp.ErrServerClosed) {
			log.Error("server exited", "err", err)
		}
	case <-rootCtx.Done():
		log.Info("shutdown signal received")
	}

	// ─── graceful shutdown ─────────────────────────────────────────
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	_ = httpSrv.Shutdown(shutdownCtx)
	_ = metricsSrv.Shutdown(shutdownCtx)
	grpcSrv.GracefulStop()

	if shutdownTracing != nil {
		_ = shutdownTracing(shutdownCtx)
	}

	log.Info("shutdown complete")
	return nil
}
