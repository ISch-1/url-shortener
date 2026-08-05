// Package postgres is the Postgres implementation of the link store.
//
// The store is the anti-corruption boundary — its methods speak in domain.Link,
// never in row scans. The service depends on this interface, never on pgx types.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ISch-1/url-shortener/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	pool *pgxpool.Pool
}

// New opens a pool and applies embedded migrations.
func New(ctx context.Context, dsn string, maxConns int32, connectTimeout time.Duration) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.ConnectTimeout = connectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool exposes the underlying pool for wiring by other packages (e.g. metrics).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("apply %s: %w", e.Name(), err)
		}
	}
	return nil
}

// Create inserts a new link. Returns domain.ErrDuplicateCode on unique
// constraint violation.
func (s *Store) Create(ctx context.Context, link domain.Link) error {
	const q = `
        INSERT INTO links
            (code, snowflake_id, original_url, api_key, created_at, expires_at, active)
        VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7)`
	_, err := s.pool.Exec(ctx, q,
		link.Code, link.SnowflakeID, link.OriginalURL, link.APIKey,
		link.CreatedAt, link.ExpiresAt, link.Active,
	)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrDuplicateCode
	}
	return err
}

// Get returns the link with the given code, or domain.ErrLinkNotFound.
func (s *Store) Get(ctx context.Context, code string) (domain.Link, error) {
	const q = `
        SELECT code, snowflake_id, original_url, COALESCE(api_key, ''), created_at, expires_at, active, click_count
        FROM links
        WHERE code = $1`
	row := s.pool.QueryRow(ctx, q, code)
	var l domain.Link
	err := row.Scan(&l.Code, &l.SnowflakeID, &l.OriginalURL, &l.APIKey,
		&l.CreatedAt, &l.ExpiresAt, &l.Active, &l.ClickCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Link{}, domain.ErrLinkNotFound
	}
	return l, err
}

// Deactivate sets active=false. Returns whether a row was touched.
func (s *Store) Deactivate(ctx context.Context, code string) (bool, error) {
	const q = `UPDATE links SET active = false WHERE code = $1 AND active = true`
	tag, err := s.pool.Exec(ctx, q, code)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// BumpClick atomically increments click_count and returns the new value.
// This is fire-and-forget from the caller's perspective — call it in a
// goroutine so it doesn't add latency to the redirect.
func (s *Store) BumpClick(ctx context.Context, code string) error {
	const q = `UPDATE links SET click_count = click_count + 1 WHERE code = $1`
	_, err := s.pool.Exec(ctx, q, code)
	return err
}
