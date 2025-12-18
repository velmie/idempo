// Package redis provides a Redis-backed implementation of idempo.Store.
//
// It uses go-redis v9 and Lua scripts to atomically:
//   - create an in-progress entry (lock)
//   - commit a completed response
//   - unlock an entry on failure
//
// A per-lock token is used to ensure only the lock owner can commit or unlock.
// The default Redis key prefix is "idempotency:" (configurable via WithKeyPrefix).
package redis
