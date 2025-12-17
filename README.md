# idempo

[![Go Reference](https://pkg.go.dev/badge/github.com/velmie/idempo.svg)](https://pkg.go.dev/github.com/velmie/idempo)
[![Go Report Card](https://goreportcard.com/badge/github.com/velmie/idempo)](https://goreportcard.com/report/github.com/velmie/idempo)
[![Go Version](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/github/license/velmie/idempo)](LICENSE)

`idempo` is a small idempotency engine and `net/http` middleware for implementing the **Idempotency-Key** pattern in Go services.

It stores the first completed response (configurable) and replays it on subsequent requests with the same key, while preventing accidental key reuse with a different request fingerprint (method + URL + body hash, optionally selected headers).

## Installation

Requires Go 1.21+.

```bash
go get github.com/velmie/idempo
```

## Quick Start (HTTP middleware)

```go
package main

import (
	"log"
	"net/http"

	"github.com/velmie/idempo"
	"github.com/velmie/idempo/middleware"
	"github.com/velmie/idempo/memory"
)

func main() {
	store := memory.New()
	defer func() { _ = store.Close() }()

	engine := idempo.NewEngine(store, idempo.WithWaitForInProgress(true))

	mw := middleware.Middleware(
		middleware.WithEngine(engine),
		middleware.WithRequireKey(true),
		middleware.WithAllowedResponseHeaders("Content-Type"),
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	log.Fatal(http.ListenAndServe(":8080", handler))
}
```

- Replays return `X-Idempotent-Replay: true`.
- Responses larger than the configured limit are streamed, stored as truncated, and replayed without a body (`X-Idempotent-Truncated: true`).

## How it works

`Idempotency-Key` request -> fingerprint -> `Engine.Process(key, fp)`:

- `Result.Response != nil` -> replay the stored response (`X-Idempotent-Replay: true`).
- `Result.IsOwner == true` -> run the handler -> `Engine.Commit(key, token, resp)` (or `Engine.Unlock(...)` on failure).
- Otherwise -> another request is in progress (default: return `ErrInProgress`, or enable waiting via `idempo.WithWaitForInProgress(true)`).

## Default HTTP behavior

Defaults in `middleware.Config` (override via `middleware.With*` options):

- Header: `Idempotency-Key` (required only when `middleware.WithRequireKey(true)` is set).
- Methods: `POST`, `PATCH`.
- Fingerprint: method + path/query (query params sorted) + SHA-256(body) + optional selected headers (`middleware.WithFingerprintHeaders(...)`).
- Store predicate: store responses with `status < 500` (`middleware.WithShouldStore(...)`).
- Stored headers: none unless allowlisted (`middleware.WithAllowedResponseHeaders(...)`).

Default error mapping (override via `middleware.WithErrorHandler(...)`):

- Missing key or invalid key -> `400`.
- Body too large for fingerprinting -> `413`.
- Same key, different fingerprint -> `422`.
- In progress / wait timeout / key not found or expired -> `409`.
- Everything else -> `500`.

## Limits and caveats

- Request bodies are buffered (up to `MaxBodyBytes`) to compute the fingerprint, larger bodies are rejected.
- This middleware is not intended for SSE/websockets or hijacked connections.
- Persisted bodies and headers can contain sensitive data; keep `WithAllowedResponseHeaders(...)` minimal.

## Features

- Pluggable storage via `idempo.Store` (`memory/` for dev/tests, `redis/` for production).
- Safe replays with request fingerprinting (method + path/query + body hash and optional selected headers).
- Configurable concurrency behavior: fail fast or wait and replay (`idempo.WithWaitForInProgress(true)`).
- Middleware controls: methods, key validation/requirement, what status codes/headers to store, size limits.

## Advanced Usage

**Redis store**

```go
import (
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/velmie/idempo"
	idemporedis "github.com/velmie/idempo/redis"
)

rdb := redisv9.NewClient(&redisv9.Options{Addr: "localhost:6379"})
store := idemporedis.New(rdb) // defaults to Redis key prefix: "idempotency:"
engine := idempo.NewEngine(store)
```

**Middleware tuning**

- Default methods: `POST`, `PATCH` (override with `middleware.WithMethods(...)`).
- Default limits: 1 MiB request body hashing + 1 MiB response buffering (override with `middleware.WithMaxBodyBytes(...)`, `middleware.WithMaxResponseBytes(...)`).
- By default, no response headers are stored; enable an allowlist via `middleware.WithAllowedResponseHeaders(...)`.
- Customize fingerprints (e.g., include user headers) via `middleware.WithFingerprintHeaders("X-User-Id")`.

See the full API on pkg.go.dev: https://pkg.go.dev/github.com/velmie/idempo

## Contributing / Development

```bash
go test -v -race ./...
docker run --rm -v "$(pwd)":/app:ro -w /app golangci/golangci-lint:v2.5.0 golangci-lint run ./...
```

## License

MIT - see [LICENSE](LICENSE).
