// Package middleware provides a net/http middleware implementing Idempotency-Key semantics.
//
// The middleware:
//   - extracts the idempotency key from a header (default: "Idempotency-Key")
//   - fingerprints the request (method + path/query + body hash; optional headers)
//   - stores eligible responses and replays them for repeated keys
//
// Replayed responses include the "X-Idempotent-Replay: true" header.
// When a response exceeds the configured buffering limit, it is streamed to the client,
// stored as truncated, and replays omit the body ("X-Idempotent-Truncated: true").
//
// This middleware is not intended for streaming responses (SSE/websockets) or hijacked
// connections; see Config for size limits and commit error modes.
package middleware

