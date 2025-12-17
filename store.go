package idempo

import (
	"context"
	"time"
)

// Entry is the stored representation of an idempotent operation.
type Entry struct {
	Key         string      `json:"key"`
	Fingerprint Fingerprint `json:"fingerprint"`
	// Token binds an acquired lock to a specific owner/lease. It must be treated as opaque.
	Token     string    `json:"token,omitempty"`
	Response  *Response `json:"response,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store defines persistence contract for idempotency records.
type Store interface {
	// Create registers a new in-progress entry with lock TTL. Returns the existing entry
	// and created=false when the key already exists and is not expired.
	Create(ctx context.Context, key string, fp Fingerprint, ttl time.Duration) (entry *Entry, created bool, err error)
	// Get returns a snapshot of the entry or ErrKeyNotFound if it does not exist or is expired.
	Get(ctx context.Context, key string) (entry *Entry, err error)
	// SetResponse stores completed response and extends TTL to result TTL.
	// Implementations must ensure that only the current lock owner (matching token) can commit.
	SetResponse(ctx context.Context, key, token string, resp *Response, ttl time.Duration) (err error)
	// Delete removes any record (in-progress or completed) for the key.
	// Engine uses this exclusively to roll back an acquired lock on failure.
	// Implementations must ensure that only the current lock owner (matching token) can unlock.
	Delete(ctx context.Context, key, token string) (err error)
}
