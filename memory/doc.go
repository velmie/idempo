// Package memory provides an in-memory implementation of idempo.Store.
//
// It is intended for tests and local development. The store is process-local and
// does not persist across restarts. It starts a background cleanup goroutine; call
// Store.Close when the store is no longer needed.
package memory
