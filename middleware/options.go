package middleware

import (
	"net/http"
	"time"

	"github.com/velmie/idempo"
)

// Option configures middleware Config via functional options.
type Option func(*Config)

// NewConfig builds Config from options, applying defaults afterwards.
func NewConfig(opts ...Option) Config {
	var cfg Config
	for _, opt := range opts {
		opt(&cfg)
	}

	return cfg.withDefaults()
}

// WithEngine sets the idempotency engine used by the middleware (required).
func WithEngine(engine *idempo.Engine) Option {
	return func(c *Config) {
		c.Engine = engine
	}
}

// WithHeaderName overrides the header name used to read idempotency keys (default: "Idempotency-Key").
func WithHeaderName(name string) Option {
	return func(c *Config) {
		c.HeaderName = name
	}
}

// WithKeyPrefix sets a prefix prepended to the stored key (useful for multi-tenant or per-service namespaces).
func WithKeyPrefix(prefix string) Option {
	return func(c *Config) {
		c.KeyPrefix = prefix
	}
}

// WithMethods limits idempotency handling to the provided HTTP methods (default: POST, PATCH).
func WithMethods(methods ...string) Option {
	return func(c *Config) {
		c.Methods = append([]string(nil), methods...)
	}
}

// WithMaxBodyBytes sets the maximum request body size (bytes) allowed for fingerprinting.
func WithMaxBodyBytes(n int64) Option {
	return func(c *Config) {
		c.MaxBodyBytes = n
	}
}

// WithMaxResponseBytes sets the maximum response body size (bytes) that is buffered for storing/replay.
func WithMaxResponseBytes(n int64) Option {
	return func(c *Config) {
		c.MaxResponseBytes = n
	}
}

// WithMaxStoredHeaderBytes sets the maximum total size of stored response headers.
func WithMaxStoredHeaderBytes(n int64) Option {
	return func(c *Config) {
		c.MaxStoredHeaderBytes = n
	}
}

// WithCommitTimeout sets the context timeout used for Engine.Commit/Unlock calls.
func WithCommitTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.CommitTimeout = d
	}
}

// WithCommitErrorMode sets what to do when persisting the idempotency record fails.
func WithCommitErrorMode(mode CommitErrorMode) Option {
	return func(c *Config) {
		c.CommitErrorMode = mode
	}
}

// WithCommitErrorHandler sets a callback invoked when Engine.Commit fails (for logging/metrics only).
func WithCommitErrorHandler(fn func(*http.Request, error)) Option {
	return func(c *Config) {
		c.CommitErrorHandler = fn
	}
}

// WithRequireKey toggles whether requests without an idempotency key should be rejected.
func WithRequireKey(require bool) Option {
	return func(c *Config) {
		c.RequireKey = require
	}
}

// WithKeyValidator overrides key validation (default: non-empty and <= 255 chars).
func WithKeyValidator(fn func(string) error) Option {
	return func(c *Config) {
		c.KeyValidator = fn
	}
}

// WithFingerprintFunc overrides how request fingerprints are built.
func WithFingerprintFunc(fn func(*http.Request) (idempo.Fingerprint, error)) Option {
	return func(c *Config) {
		c.FingerprintFunc = fn
	}
}

// WithFingerprintHeaders configures which request headers participate in the fingerprint.
// Values are canonicalized and sorted for stability.
func WithFingerprintHeaders(headers ...string) Option {
	return func(c *Config) {
		c.FingerprintHeaders = append([]string(nil), headers...)
	}
}

// WithShouldHashBody controls whether a request body should be hashed for the fingerprint.
func WithShouldHashBody(fn func(*http.Request) bool) Option {
	return func(c *Config) {
		c.ShouldHashBody = fn
	}
}

// WithAllowedResponseHeaders sets an allowlist of response headers to store and replay.
// When empty, no response headers are stored. Hop-by-hop headers are always excluded.
func WithAllowedResponseHeaders(headers ...string) Option {
	return func(c *Config) {
		c.AllowedResponseHeaders = append([]string(nil), headers...)
	}
}

// WithShouldStore overrides the predicate controlling which responses are stored (by status code).
func WithShouldStore(fn func(status int) bool) Option {
	return func(c *Config) {
		c.ShouldStore = fn
	}
}

// WithErrorHandler overrides how middleware errors are translated to HTTP responses.
func WithErrorHandler(fn func(http.ResponseWriter, *http.Request, error)) Option {
	return func(c *Config) {
		c.ErrorHandler = fn
	}
}
