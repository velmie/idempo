package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/velmie/idempo"
)

const (
	defaultMaxPayloadBytes      int64 = 1 << 20
	defaultMaxStoredHeaderBytes       = 64 * 1024
	defaultKeyLength                  = 255
	defaultCommitTimeout              = 5 * time.Second
)

// CommitErrorMode defines middleware behavior when Engine.Commit fails.
type CommitErrorMode int

const (
	// CommitFailOpen writes the handler response to the client and unlocks the key.
	// Idempotency degrades for the key when commit fails.
	CommitFailOpen CommitErrorMode = iota
	// CommitFailClosedUnlock responds with 500 (when possible) and unlocks the key, allowing retries.
	CommitFailClosedUnlock
	// CommitFailClosedKeepLock responds with 500 (when possible) and keeps the lock until it expires.
	CommitFailClosedKeepLock
)

// Config defines behavior of HTTP adapter.
type Config struct {
	Engine *idempo.Engine

	HeaderName string
	KeyPrefix  string
	Methods    []string

	MaxBodyBytes         int64
	MaxResponseBytes     int64
	MaxStoredHeaderBytes int64
	CommitTimeout        time.Duration
	CommitErrorMode      CommitErrorMode
	// CommitErrorHandler is called when committing the idempotency record fails.
	// It should be used for logging/metrics only; the handler must not write to the response.
	CommitErrorHandler func(*http.Request, error)
	// Note: request bodies are buffered up to MaxBodyBytes for fingerprinting.
	// Responses are buffered up to MaxResponseBytes; larger responses are streamed but stored as truncated (no body).
	// This middleware is not intended for streaming responses (SSE/websockets) or hijacked connections.
	// Large request bodies beyond MaxBodyBytes are rejected.

	RequireKey   bool
	KeyValidator func(string) error

	FingerprintFunc    func(*http.Request) (idempo.Fingerprint, error)
	FingerprintHeaders []string
	ShouldHashBody     func(*http.Request) bool
	// AllowedResponseHeaders limits which response headers are stored for replay.
	// When empty, no response headers are stored.
	// Hop-by-hop headers are always excluded regardless of allowlist.
	AllowedResponseHeaders []string

	ShouldStore  func(status int) bool
	ErrorHandler func(http.ResponseWriter, *http.Request, error)
}

func (c Config) withDefaults() Config {
	if c.HeaderName == "" {
		c.HeaderName = "Idempotency-Key"
	}
	if len(c.Methods) == 0 {
		c.Methods = []string{http.MethodPost, http.MethodPatch}
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = defaultMaxPayloadBytes
	}
	if c.MaxResponseBytes <= 0 {
		c.MaxResponseBytes = defaultMaxPayloadBytes
	}
	if c.MaxStoredHeaderBytes <= 0 {
		c.MaxStoredHeaderBytes = defaultMaxStoredHeaderBytes
	}
	if c.CommitTimeout <= 0 {
		c.CommitTimeout = defaultCommitTimeout
	}
	if c.KeyValidator == nil {
		c.KeyValidator = defaultKeyValidator
	}
	for i, h := range c.FingerprintHeaders {
		c.FingerprintHeaders[i] = http.CanonicalHeaderKey(strings.TrimSpace(h))
	}
	if c.ShouldHashBody == nil {
		c.ShouldHashBody = func(*http.Request) bool { return true }
	}
	if c.ShouldStore == nil {
		c.ShouldStore = func(status int) bool { return status < http.StatusInternalServerError }
	}
	if c.ErrorHandler == nil {
		c.ErrorHandler = defaultErrorHandler(c.HeaderName)
	}

	return c
}

func defaultKeyValidator(k string) error {
	k = strings.TrimSpace(k)
	if k == "" {
		return idempo.ErrMissingKey
	}
	if len(k) > defaultKeyLength {
		return fmt.Errorf("%w: too long (max %d chars)", idempo.ErrInvalidKey, defaultKeyLength)
	}

	return nil
}

func defaultErrorHandler(headerName string) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, _ *http.Request, err error) {
		switch {
		case errors.Is(err, idempo.ErrMissingKey):
			http.Error(w, headerName+" header is required", http.StatusBadRequest)
		case errors.Is(err, idempo.ErrInvalidKey):
			http.Error(w, "invalid "+headerName+" header", http.StatusBadRequest)
		case errors.Is(err, idempo.ErrBodyTooLarge):
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, idempo.ErrKeyNotFound),
			errors.Is(err, idempo.ErrKeyExpired):
			http.Error(w, "idempotency record not found or expired", http.StatusConflict)
		case errors.Is(err, idempo.ErrKeyConflict):
			http.Error(w, headerName+" is already used for a different request payload", http.StatusUnprocessableEntity)
		case errors.Is(err, idempo.ErrInProgress),
			errors.Is(err, idempo.ErrWaitTimeout):
			http.Error(w, "another request with the same "+headerName+" is in progress", http.StatusConflict)
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}
