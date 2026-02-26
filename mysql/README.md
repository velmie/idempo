# idempo/mysql

MySQL-backed `idempo.Store` implementation.

## WARNING

`parseTime=true` in DSN is mandatory.

Without `parseTime=true`, `go-sql-driver/mysql` returns `[]byte` for `DATETIME` columns and scanning into `time.Time` fails.

Use DSN with:

- `parseTime=true` (required)
- `loc=UTC` (strongly recommended)

## Install

```bash
go get github.com/velmie/idempo/mysql
```

## Requirements

- MySQL 5.7+
- DSN options for `go-sql-driver/mysql`: `parseTime=true&loc=UTC`

## Schema

```sql
CREATE TABLE idempo_entries (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,

  idem_key VARBINARY(512) NOT NULL,
  token VARBINARY(32) NOT NULL,

  fp_operation VARCHAR(16) NOT NULL,
  fp_target TEXT NOT NULL,
  fp_headers_hash VARCHAR(64) NOT NULL,
  fp_body_hash VARCHAR(64) NOT NULL,

  resp_status_code INT NULL,
  resp_truncated TINYINT(1) NOT NULL DEFAULT 0,
  resp_metadata BLOB NULL,
  resp_body MEDIUMBLOB NULL,

  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  expires_at DATETIME(6) NOT NULL,

  PRIMARY KEY (id),
  UNIQUE KEY uq_idem_key (idem_key),
  KEY idx_expires_at (expires_at)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_bin;
```

## Notes

- TTL is enforced with `expires_at` using database time (`UTC_TIMESTAMP(6)`).
- `SetResponse` and `Delete` are guarded by `token` + alive lease (`expires_at > now`).
- Optional cleanup loop can be enabled via `WithCleanup`; for multi-instance safety it uses `GET_LOCK`.

## Tests

```bash
go test ./...
go test -tags=integration ./...
```
