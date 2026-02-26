// Package mysql provides a MySQL-backed implementation of idempo.Store.
//
// WARNING:
//   - DSN must include parseTime=true.
//   - Without parseTime=true, DATETIME columns are returned as []byte by go-sql-driver/mysql
//     and scanning into time.Time fails.
//   - loc=UTC is strongly recommended.
//
// Notes:
//   - TTL is implemented via expires_at.
//   - Time comparisons use database time (UTC_TIMESTAMP(6)) to avoid clock skew issues.
//   - New key: single INSERT.
//   - Duplicate key: usually single SELECT.
//   - Expired key takeover: single UPDATE with expires_at guard.
//   - Cleanup is optional and can use advisory lock (GET_LOCK) for multi-instance safety.
package mysql
