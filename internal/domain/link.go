// Package domain defines the core types. No storage, no transport — pure Go.
package domain

import (
	"errors"
	"time"
)

// Link is the canonical shape of a shortened URL.
type Link struct {
	Code        string // base62-encoded Snowflake id
	SnowflakeID int64  // the raw id
	OriginalURL string // the URL to redirect to
	APIKey      string // owner, if any
	CreatedAt   time.Time
	ExpiresAt   *time.Time // optional — nil means never
	Active      bool
	ClickCount  int64
}

// IsExpired reports whether the link has an expiry and it has passed.
func (l Link) IsExpired(now time.Time) bool {
	return l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

// Errors returned by the storage and service layers. Callers pattern-match with
// errors.Is; HTTP + gRPC translate to appropriate status codes.
var (
	ErrLinkNotFound  = errors.New("link not found")
	ErrLinkInactive  = errors.New("link inactive")
	ErrLinkExpired   = errors.New("link expired")
	ErrInvalidURL    = errors.New("invalid url")
	ErrRateLimited   = errors.New("rate limited")
	ErrDuplicateCode = errors.New("duplicate code")
)
