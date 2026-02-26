package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/velmie/idempo"
)

const (
	queryCreateFmt = `
INSERT INTO %s (
  idem_key, token,
  fp_operation, fp_target, fp_headers_hash, fp_body_hash,
  resp_status_code, resp_truncated, resp_metadata, resp_body,
  created_at, updated_at, expires_at
) VALUES (
  ?, ?,
  ?, ?, ?, ?,
  NULL, 0, NULL, NULL,
  UTC_TIMESTAMP(6), UTC_TIMESTAMP(6),
  TIMESTAMPADD(MICROSECOND, ?, UTC_TIMESTAMP(6))
)`

	querySelectFmt = `
SELECT
  token,
  fp_operation, fp_target, fp_headers_hash, fp_body_hash,
  created_at, updated_at,
  resp_status_code, resp_truncated, resp_metadata, resp_body
FROM %s
WHERE idem_key = ? AND expires_at > UTC_TIMESTAMP(6)`

	queryTakeoverFmt = `
UPDATE %s
SET
  token = ?,
  fp_operation = ?, fp_target = ?, fp_headers_hash = ?, fp_body_hash = ?,
  resp_status_code = NULL, resp_truncated = 0, resp_metadata = NULL, resp_body = NULL,
  created_at = UTC_TIMESTAMP(6),
  updated_at = UTC_TIMESTAMP(6),
  expires_at = TIMESTAMPADD(MICROSECOND, ?, UTC_TIMESTAMP(6))
WHERE idem_key = ? AND expires_at <= UTC_TIMESTAMP(6)`

	queryCommitFmt = `
UPDATE %s
SET
  resp_status_code = ?,
  resp_truncated = ?,
  resp_metadata = ?,
  resp_body = ?,
  updated_at = UTC_TIMESTAMP(6),
  expires_at = TIMESTAMPADD(MICROSECOND, ?, UTC_TIMESTAMP(6))
WHERE idem_key = ? AND token = ? AND expires_at > UTC_TIMESTAMP(6)`

	queryDeleteFmt = `
DELETE FROM %s
WHERE idem_key = ? AND token = ? AND expires_at > UTC_TIMESTAMP(6)`

	queryCleanupFmt = `
DELETE FROM %s
WHERE expires_at <= UTC_TIMESTAMP(6)
ORDER BY expires_at
LIMIT ?`

	queryAcquireCleanupLock = `SELECT GET_LOCK(?, 0)`
	queryReleaseCleanupLock = `DO RELEASE_LOCK(?)`

	duplicateKeyCode = uint16(1062)
	maxCreateRetries = 3
)

// Store implements idempo.Store backed by MySQL.
type Store struct {
	db   *sql.DB
	opts Options

	createQuery   string
	selectQuery   string
	takeoverQuery string
	commitQuery   string
	deleteQuery   string
	cleanupQuery  string

	stopCh    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

var _ idempo.Store = (*Store)(nil)

// New constructs a MySQL store.
//
// SQL statements are executed lazily via database/sql (without eager prepare),
// so temporary DB unavailability at startup does not fail store initialization.
func New(db *sql.DB, opts ...Option) (*Store, error) {
	if db == nil {
		return nil, errors.New("idempo/mysql: nil *sql.DB")
	}

	cfg := Options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg.withDefaults()

	table, err := quoteTable(cfg.Table)
	if err != nil {
		return nil, err
	}

	s := &Store{
		db:            db,
		opts:          cfg,
		createQuery:   fmt.Sprintf(queryCreateFmt, table),
		selectQuery:   fmt.Sprintf(querySelectFmt, table),
		takeoverQuery: fmt.Sprintf(queryTakeoverFmt, table),
		commitQuery:   fmt.Sprintf(queryCommitFmt, table),
		deleteQuery:   fmt.Sprintf(queryDeleteFmt, table),
		cleanupQuery:  fmt.Sprintf(queryCleanupFmt, table),
		stopCh:        make(chan struct{}),
	}

	if cfg.CleanupInterval > 0 {
		s.wg.Add(1)
		go s.cleanupLoop()
	}

	return s, nil
}

// Create registers a new in-progress entry with lock TTL.
func (s *Store) Create(
	ctx context.Context,
	key string,
	fp idempo.Fingerprint,
	ttl time.Duration,
) (*idempo.Entry, bool, error) {
	ttlUS, err := ttlMicroseconds(ttl)
	if err != nil {
		return nil, false, err
	}

	for attempt := 0; attempt < maxCreateRetries; attempt++ {
		token, err := newToken()
		if err != nil {
			return nil, false, err
		}

		_, err = s.db.ExecContext(
			ctx,
			s.createQuery,
			key,
			token,
			fp.Operation,
			fp.Target,
			fp.HeadersHash,
			fp.BodyHash,
			ttlUS,
		)
		if err == nil {
			now := s.opts.Now()

			return &idempo.Entry{
				Key:         key,
				Fingerprint: fp,
				Token:       token,
				CreatedAt:   now,
				UpdatedAt:   now,
			}, true, nil
		}
		if !isDuplicateKey(err) {
			return nil, false, fmt.Errorf("idempo/mysql: create insert failed: %w", err)
		}

		entry, getErr := s.getAlive(ctx, key)
		if getErr == nil {
			return entry, false, nil
		}
		if !errors.Is(getErr, idempo.ErrKeyNotFound) {
			return nil, false, getErr
		}

		res, err := s.db.ExecContext(
			ctx,
			s.takeoverQuery,
			token,
			fp.Operation,
			fp.Target,
			fp.HeadersHash,
			fp.BodyHash,
			ttlUS,
			key,
		)
		if err != nil {
			return nil, false, fmt.Errorf("idempo/mysql: create takeover failed: %w", err)
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return nil, false, fmt.Errorf("idempo/mysql: create takeover rows: %w", err)
		}
		if rows == 1 {
			now := s.opts.Now()

			return &idempo.Entry{
				Key:         key,
				Fingerprint: fp,
				Token:       token,
				CreatedAt:   now,
				UpdatedAt:   now,
			}, true, nil
		}
	}

	entry, err := s.getAlive(ctx, key)
	if err == nil {
		return entry, false, nil
	}
	if errors.Is(err, idempo.ErrKeyNotFound) {
		return nil, false, fmt.Errorf("idempo/mysql: create lost race for key %q: %w", key, idempo.ErrKeyNotFound)
	}

	return nil, false, err
}

// Get returns a snapshot of an alive entry.
func (s *Store) Get(ctx context.Context, key string) (*idempo.Entry, error) {
	return s.getAlive(ctx, key)
}

// SetResponse stores completed response and extends key TTL to result TTL.
func (s *Store) SetResponse(
	ctx context.Context,
	key, token string,
	resp *idempo.Response,
	ttl time.Duration,
) error {
	if resp == nil {
		return idempo.ErrResponseNil
	}

	ttlUS, err := ttlMicroseconds(ttl)
	if err != nil {
		return err
	}

	var metadata []byte
	if len(resp.Metadata) > 0 {
		metadata, err = json.Marshal(resp.Metadata)
		if err != nil {
			return fmt.Errorf("idempo/mysql: marshal response metadata: %w", err)
		}
	}

	body := resp.Body
	if resp.Truncated {
		body = nil
	}

	res, err := s.db.ExecContext(
		ctx,
		s.commitQuery,
		resp.StatusCode,
		resp.Truncated,
		metadata,
		body,
		ttlUS,
		key,
		token,
	)
	if err != nil {
		return fmt.Errorf("idempo/mysql: commit failed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("idempo/mysql: commit rows: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	}

	return nil
}

// Delete removes an entry only when key/token still belong to an alive owner lease.
func (s *Store) Delete(ctx context.Context, key, token string) error {
	res, err := s.db.ExecContext(ctx, s.deleteQuery, key, token)
	if err != nil {
		return fmt.Errorf("idempo/mysql: delete failed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("idempo/mysql: delete rows: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	}

	return nil
}

// Close stops the optional cleanup goroutine.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		s.wg.Wait()
	})

	return nil
}

func (s *Store) getAlive(ctx context.Context, key string) (*idempo.Entry, error) {
	var (
		token string

		op       string
		target   string
		hdrHash  string
		bodyHash string

		createdAt time.Time
		updatedAt time.Time

		status    sql.NullInt64
		truncated bool
		metaBytes []byte
		body      []byte
	)

	err := s.db.QueryRowContext(ctx, s.selectQuery, key).Scan(
		&token,
		&op,
		&target,
		&hdrHash,
		&bodyHash,
		&createdAt,
		&updatedAt,
		&status,
		&truncated,
		&metaBytes,
		&body,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, idempo.ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("idempo/mysql: get failed: %w", err)
	}

	entry := &idempo.Entry{
		Key: key,
		Fingerprint: idempo.Fingerprint{
			Operation:   op,
			Target:      target,
			HeadersHash: hdrHash,
			BodyHash:    bodyHash,
		},
		Token:     token,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	if status.Valid {
		response := &idempo.Response{
			StatusCode: int(status.Int64),
			Truncated:  truncated,
		}
		if len(metaBytes) > 0 {
			if err := json.Unmarshal(metaBytes, &response.Metadata); err != nil {
				return nil, fmt.Errorf("idempo/mysql: unmarshal response metadata: %w", err)
			}
		}
		if !truncated && len(body) > 0 {
			response.Body = append([]byte(nil), body...)
		}
		entry.Response = response
	}

	return entry, nil
}

func (s *Store) cleanupLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.opts.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupOnce(context.Background())
		case <-s.stopCh:
			return
		}
	}
}

func (s *Store) cleanupOnce(parent context.Context) {
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	if s.opts.CleanupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.opts.CleanupTimeout)
		defer cancel()
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		s.onCleanupError(fmt.Errorf("idempo/mysql: cleanup acquire conn: %w", err))
		return
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			s.onCleanupError(fmt.Errorf("idempo/mysql: cleanup conn close: %w", closeErr))
		}
	}()

	acquired, err := s.acquireCleanupLock(ctx, conn)
	if err != nil {
		s.onCleanupError(err)
		return
	}
	if !acquired {
		return
	}

	defer func() {
		releaseErr := s.releaseCleanupLock(context.Background(), conn)
		if releaseErr != nil {
			s.onCleanupError(releaseErr)
		}
	}()

	_, err = conn.ExecContext(ctx, s.cleanupQuery, s.opts.CleanupBatch)
	if err != nil {
		s.onCleanupError(fmt.Errorf("idempo/mysql: cleanup delete: %w", err))
	}
}

func (s *Store) acquireCleanupLock(ctx context.Context, conn *sql.Conn) (bool, error) {
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, queryAcquireCleanupLock, s.opts.CleanupLockName).Scan(&acquired); err != nil {
		return false, fmt.Errorf("idempo/mysql: cleanup GET_LOCK: %w", err)
	}
	if !acquired.Valid {
		return false, fmt.Errorf("idempo/mysql: cleanup GET_LOCK returned NULL for %q", s.opts.CleanupLockName)
	}
	if acquired.Int64 == 1 {
		return true, nil
	}
	if acquired.Int64 == 0 {
		return false, nil
	}

	return false, fmt.Errorf("idempo/mysql: cleanup GET_LOCK returned unexpected value %d", acquired.Int64)
}

func (s *Store) releaseCleanupLock(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, queryReleaseCleanupLock, s.opts.CleanupLockName)
	if err != nil {
		return fmt.Errorf("idempo/mysql: cleanup RELEASE_LOCK: %w", err)
	}

	return nil
}

func (s *Store) onCleanupError(err error) {
	if err == nil || s.opts.OnCleanupError == nil {
		return
	}

	s.opts.OnCleanupError(err)
}

func ttlMicroseconds(ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, fmt.Errorf("idempo/mysql: invalid ttl: %v", ttl)
	}

	us := ttl.Microseconds()
	if us <= 0 {
		us = 1
	}

	return us, nil
}

func newToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("idempo/mysql: generate token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == duplicateKeyCode
	}

	msg := err.Error()

	return strings.Contains(msg, "Error 1062") || strings.Contains(msg, "Duplicate entry")
}

func quoteTable(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("idempo/mysql: empty table name")
	}

	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("idempo/mysql: invalid table name %q", name)
	}
	for _, p := range parts {
		if !isIdent(p) {
			return "", fmt.Errorf("idempo/mysql: invalid table name %q", name)
		}
	}

	if len(parts) == 1 {
		return "`" + parts[0] + "`", nil
	}

	return "`" + parts[0] + "`.`" + parts[1] + "`", nil
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}

	return true
}
