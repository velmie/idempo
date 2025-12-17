package idempo

import "errors"

var (
	// ErrWaitTimeout indicates that the wait time for an in-flight operation expired.
	ErrWaitTimeout = errors.New("timeout while waiting for idempotent operation to complete")
	// ErrKeyConflict is returned when the same idempotency key is reused with a different fingerprint.
	ErrKeyConflict = errors.New("idempotency key is already used for a different fingerprint")
	// ErrInProgress signals that another operation with the same idempotency key is currently running.
	ErrInProgress = errors.New("another operation with the same idempotency key is in progress")
	// ErrMissingKey is returned when an adapter requires the key but it is not provided.
	ErrMissingKey = errors.New("idempotency key is required")
	// ErrInvalidKey denotes that the provided key does not pass validation.
	ErrInvalidKey = errors.New("invalid idempotency key")
	// ErrBodyTooLarge is returned when the request body exceeds configured limits.
	ErrBodyTooLarge = errors.New("request body too large for idempotency middleware")
	// ErrKeyNotFound is returned when the store cannot find the given idempotency key.
	ErrKeyNotFound = errors.New("idempotency key not found")
	// ErrKeyExpired is returned when the store key expired before the operation completed.
	ErrKeyExpired = errors.New("idempotency key expired")
	// ErrResponseNil is returned when attempting to commit a nil response.
	ErrResponseNil = errors.New("idempotency response is required")
)
