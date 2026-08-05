// Package http hosts the REST surface — thin handlers over the service.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ISch-1/url-shortener/internal/domain"
	"github.com/ISch-1/url-shortener/internal/service"
)

type Handlers struct {
	svc     *service.Shortener
	baseURL string
	log     *slog.Logger
}

func NewHandlers(svc *service.Shortener, baseURL string, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, baseURL: baseURL, log: log}
}

// createLinkReq is the JSON body for POST /api/v1/links.
type createLinkReq struct {
	OriginalURL string     `json:"originalUrl"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

type linkDTO struct {
	Code        string     `json:"code"`
	ShortURL    string     `json:"shortUrl"`
	OriginalURL string     `json:"originalUrl"`
	SnowflakeID int64      `json:"snowflakeId"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Active      bool       `json:"active"`
	ClickCount  int64      `json:"clickCount"`
}

func (h *Handlers) toDTO(l domain.Link) linkDTO {
	return linkDTO{
		Code:        l.Code,
		ShortURL:    h.baseURL + "/" + l.Code,
		OriginalURL: l.OriginalURL,
		SnowflakeID: l.SnowflakeID,
		CreatedAt:   l.CreatedAt,
		ExpiresAt:   l.ExpiresAt,
		Active:      l.Active,
		ClickCount:  l.ClickCount,
	}
}

// POST /api/v1/links
func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	var body createLinkReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	link, err := h.svc.Create(r.Context(), service.CreateInput{
		OriginalURL: body.OriginalURL,
		APIKey:      r.Header.Get("X-Api-Key"),
		ExpiresAt:   body.ExpiresAt,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidURL) {
			writeError(w, http.StatusBadRequest, "invalid_url", err.Error())
			return
		}
		h.log.ErrorContext(r.Context(), "create failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected error")
		return
	}
	writeJSON(w, http.StatusCreated, h.toDTO(link))
}

// GET /api/v1/links/{code}
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	link, err := h.svc.Get(r.Context(), code)
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "link not found")
			return
		}
		h.log.ErrorContext(r.Context(), "get failed", "err", err, "code", code)
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected error")
		return
	}
	writeJSON(w, http.StatusOK, h.toDTO(link))
}

// DELETE /api/v1/links/{code}
func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	removed, err := h.svc.Deactivate(r.Context(), code)
	if err != nil {
		h.log.ErrorContext(r.Context(), "deactivate failed", "err", err, "code", code)
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected error")
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "not_found", "link not found or already inactive")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /{code} — 301 redirect.
func (h *Handlers) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	url, ok, err := h.svc.Resolve(r.Context(), code)
	if err != nil {
		if errors.Is(err, domain.ErrLinkExpired) || errors.Is(err, domain.ErrLinkInactive) {
			writeError(w, http.StatusGone, "gone", err.Error())
			return
		}
		h.log.ErrorContext(r.Context(), "resolve failed", "err", err, "code", code)
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected error")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	}

	// Fire-and-forget click bump — do not block the redirect on it.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.svc.BumpClick(ctx, code)
	}()

	http.Redirect(w, r, url, http.StatusMovedPermanently)
}

// ────────────────────────────── helpers ──────────────────────────────

type errorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResp{Code: code, Message: msg})
}
