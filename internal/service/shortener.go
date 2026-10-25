// Package service implements the application logic. Transport-agnostic — HTTP
// and gRPC layers both call this same type.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/ISch-1/url-shortener/internal/cache"
	"github.com/ISch-1/url-shortener/internal/domain"
	"github.com/ISch-1/url-shortener/internal/idgen"
	"github.com/ISch-1/url-shortener/internal/storage/postgres"
)

// Metrics is the small hook the service uses to publish counters. The
// concrete implementation lives in the telemetry package to keep this file
// free of Prometheus imports.
type Metrics interface {
	CacheHit()
	CacheMiss()
	SingleflightCall()
	SingleflightShared()
	LinkCreated()
	LinkDeactivated()
}

type noopMetrics struct{}

func (noopMetrics) CacheHit()           {}
func (noopMetrics) CacheMiss()          {}
func (noopMetrics) SingleflightCall()   {}
func (noopMetrics) SingleflightShared() {}
func (noopMetrics) LinkCreated()        {}
func (noopMetrics) LinkDeactivated()    {}

// Shortener is the application service.
type Shortener struct {
	store   *postgres.Store
	cache   *cache.LinkCache
	gen     *idgen.Snowflake
	metrics Metrics

	sf singleflight.Group

	now func() time.Time
}

func New(store *postgres.Store, cache *cache.LinkCache, gen *idgen.Snowflake, m Metrics) *Shortener {
	if m == nil {
		m = noopMetrics{}
	}
	return &Shortener{store: store, cache: cache, gen: gen, metrics: m, now: time.Now}
}

// CreateInput is the flat request for CreateLink.
type CreateInput struct {
	OriginalURL string
	APIKey      string
	ExpiresAt   *time.Time
}

// Create issues a new short code and persists it. Retries once on the
// astronomically-unlikely code collision.
func (s *Shortener) Create(ctx context.Context, in CreateInput) (domain.Link, error) {
	if err := validateURL(in.OriginalURL); err != nil {
		return domain.Link{}, err
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return domain.Link{}, fmt.Errorf("expires_at must be in the future: %w", domain.ErrInvalidURL)
	}

	for attempt := 0; attempt < 2; attempt++ {
		id, err := s.gen.Next()
		if err != nil {
			return domain.Link{}, err
		}
		link := domain.Link{
			Code:        idgen.EncodeBase62(id),
			SnowflakeID: id,
			OriginalURL: in.OriginalURL,
			APIKey:      in.APIKey,
			CreatedAt:   s.now().UTC(),
			ExpiresAt:   in.ExpiresAt,
			Active:      true,
		}
		if err := s.store.Create(ctx, link); err != nil {
			if errors.Is(err, domain.ErrDuplicateCode) {
				continue // vanishingly rare — retry with a fresh id
			}
			return domain.Link{}, err
		}
		s.metrics.LinkCreated()
		// No cache write on create — the read path will backfill on first redirect.
		return link, nil
	}
	return domain.Link{}, errors.New("failed to allocate a unique code after retries")
}

// Get returns the link. Cache-aside with singleflight — see ADR 0003.
func (s *Shortener) Get(ctx context.Context, code string) (domain.Link, error) {
	if l, err := s.cache.Get(ctx, code); err == nil && l != nil {
		s.metrics.CacheHit()
		return *l, nil
	}
	s.metrics.CacheMiss()

	s.metrics.SingleflightCall()
	v, err, shared := s.sf.Do(code, func() (any, error) {
		l, err := s.store.Get(ctx, code)
		if err != nil {
			return nil, err
		}
		// Best-effort cache backfill — a cache write failure doesn't fail the read.
		_ = s.cache.Set(ctx, l)
		return l, nil
	})
	if shared {
		s.metrics.SingleflightShared()
	}
	if err != nil {
		return domain.Link{}, err
	}
	return v.(domain.Link), nil
}

// Resolve returns just the URL to redirect to plus a bool. Applies expiry +
// active checks. Does NOT bump the click counter — callers do that async.
func (s *Shortener) Resolve(ctx context.Context, code string) (string, bool, error) {
	link, err := s.Get(ctx, code)
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if !link.Active {
		return "", false, domain.ErrLinkInactive
	}
	if link.IsExpired(s.now()) {
		return "", false, domain.ErrLinkExpired
	}
	return link.OriginalURL, true, nil
}

// BumpClick increments the click count. Non-blocking usage: `go svc.BumpClick(ctx, code)`.
func (s *Shortener) BumpClick(ctx context.Context, code string) error {
	return s.store.BumpClick(ctx, code)
}

// Deactivate marks a link inactive and evicts the cache entry.
func (s *Shortener) Deactivate(ctx context.Context, code string) (bool, error) {
	removed, err := s.store.Deactivate(ctx, code)
	if err != nil {
		return false, err
	}
	if removed {
		s.metrics.LinkDeactivated()
		_ = s.cache.Delete(ctx, code)
	}
	return removed, nil
}

func validateURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("empty url: %w", domain.ErrInvalidURL)
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("parse: %w: %w", err, domain.ErrInvalidURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q must be http or https: %w", u.Scheme, domain.ErrInvalidURL)
	}
	if u.Host == "" {
		return fmt.Errorf("missing host: %w", domain.ErrInvalidURL)
	}
	return nil
}
