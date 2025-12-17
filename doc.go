// Package idempo provides an idempotency engine built around a pluggable Store.
//
// It is intended for implementing the Idempotency-Key pattern in services:
// for a given key, only one caller becomes the "owner" (lock holder), commits the
// result, and all subsequent callers replay the stored response.
//
// Typical flow:
//  1. Build a Fingerprint for the incoming operation (method, target, body hash, ...).
//  2. Call Engine.Process(ctx, key, fp).
//  3. If Result.Response != nil, replay it and return.
//  4. If Result.IsOwner is true, perform the operation and finalize:
//     - Engine.Commit(ctx, key, Result.Token, resp) on success
//     - Engine.Unlock(ctx, key, Result.Token) on failure
//
// Fingerprints protect against accidental key reuse: if the same key is used with a
// different fingerprint, Engine.Process returns ErrKeyConflict.
//
// For HTTP usage, see the middleware package.
package idempo

