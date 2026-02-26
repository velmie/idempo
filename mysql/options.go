package mysql

import "time"

const (
	defaultTable          = "idempo_entries"
	defaultCleanupBatch   = 1000
	defaultCleanupTimeout = 15 * time.Second
	defaultCleanupLock    = "idempo_cleanup"
)

// Options configure MySQL store behavior.
type Options struct {
	Table string

	// Now is used only to populate returned Entry timestamps on the fast path.
	// Database time is used for correctness of expiry checks and expiry writes.
	Now func() time.Time

	// Optional cleanup loop. Disabled by default when CleanupInterval <= 0.
	CleanupInterval time.Duration
	CleanupBatch    int
	CleanupTimeout  time.Duration
	CleanupLockName string
	OnCleanupError  func(error)
}

// Option mutates store options.
type Option func(*Options)

// WithTable overrides table name (supports "table" or "schema.table").
func WithTable(name string) Option {
	return func(o *Options) {
		o.Table = name
	}
}

// WithNow overrides time source used for fast-path timestamps.
func WithNow(now func() time.Time) Option {
	return func(o *Options) {
		o.Now = now
	}
}

// WithCleanup enables periodic cleanup of expired rows.
func WithCleanup(interval time.Duration, batch int) Option {
	return func(o *Options) {
		o.CleanupInterval = interval
		o.CleanupBatch = batch
	}
}

// WithCleanupTimeout sets timeout for one cleanup tick.
func WithCleanupTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.CleanupTimeout = d
	}
}

// WithCleanupLockName overrides advisory lock name used by cleanup.
func WithCleanupLockName(name string) Option {
	return func(o *Options) {
		o.CleanupLockName = name
	}
}

// WithCleanupErrorHandler sets cleanup error callback.
func WithCleanupErrorHandler(fn func(error)) Option {
	return func(o *Options) {
		o.OnCleanupError = fn
	}
}

func (o *Options) withDefaults() {
	if o.Table == "" {
		o.Table = defaultTable
	}
	if o.Now == nil {
		o.Now = func() time.Time {
			return time.Now().UTC()
		}
	}
	if o.CleanupBatch <= 0 {
		o.CleanupBatch = defaultCleanupBatch
	}
	if o.CleanupTimeout <= 0 {
		o.CleanupTimeout = defaultCleanupTimeout
	}
	if o.CleanupLockName == "" {
		o.CleanupLockName = defaultCleanupLock
	}
}
