package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/velmie/idempo"
)

func TestNewNilDB(t *testing.T) {
	t.Parallel()

	store, err := New(nil)
	if err == nil {
		t.Fatalf("expected error, got store=%v", store)
	}
}

func TestCreateInsertSuccess(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	fp := idempo.Fingerprint{Operation: "POST", Target: "/v1/orders", HeadersHash: "h1", BodyHash: "h2"}
	ttl := 5 * time.Second

	mock.ExpectExec(regexp.QuoteMeta(store.createQuery)).
		WithArgs("key-1", sqlmock.AnyArg(), fp.Operation, fp.Target, fp.HeadersHash, fp.BodyHash, ttl.Microseconds()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	entry, created, err := store.Create(context.Background(), "key-1", fp, ttl)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if entry == nil || entry.Token == "" {
		t.Fatalf("expected token in created entry, got %#v", entry)
	}
	if entry.Fingerprint != fp {
		t.Fatalf("fingerprint mismatch: got=%+v want=%+v", entry.Fingerprint, fp)
	}

	assertExpectations(t, mock)
}

func TestCreateDuplicateReturnsAliveEntry(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	fp := idempo.Fingerprint{Operation: "POST", Target: "/v1/orders", HeadersHash: "h1", BodyHash: "h2"}
	dupErr := &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry"}

	mock.ExpectExec(regexp.QuoteMeta(store.createQuery)).
		WithArgs("key-2", sqlmock.AnyArg(), fp.Operation, fp.Target, fp.HeadersHash, fp.BodyHash, time.Microsecond.Microseconds()).
		WillReturnError(dupErr)

	createdAt := time.Now().UTC().Add(-time.Second)
	updatedAt := createdAt.Add(100 * time.Millisecond)

	rows := sqlmock.NewRows(selectColumns()).
		AddRow("tok-2", fp.Operation, fp.Target, fp.HeadersHash, fp.BodyHash, createdAt, updatedAt, nil, false, nil, nil)
	mock.ExpectQuery(regexp.QuoteMeta(store.selectQuery)).WithArgs("key-2").WillReturnRows(rows)

	entry, created, err := store.Create(context.Background(), "key-2", fp, time.Microsecond)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created {
		t.Fatalf("expected created=false")
	}
	if entry == nil || entry.Token != "tok-2" {
		t.Fatalf("unexpected entry: %#v", entry)
	}

	assertExpectations(t, mock)
}

func TestCreateDuplicateThenTakeover(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	fp := idempo.Fingerprint{Operation: "PATCH", Target: "/v1/orders/1", HeadersHash: "h3", BodyHash: "h4"}
	ttl := 2 * time.Second
	dupErr := &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry"}

	mock.ExpectExec(regexp.QuoteMeta(store.createQuery)).
		WithArgs("key-3", sqlmock.AnyArg(), fp.Operation, fp.Target, fp.HeadersHash, fp.BodyHash, ttl.Microseconds()).
		WillReturnError(dupErr)

	mock.ExpectQuery(regexp.QuoteMeta(store.selectQuery)).WithArgs("key-3").WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(store.takeoverQuery)).
		WithArgs(sqlmock.AnyArg(), fp.Operation, fp.Target, fp.HeadersHash, fp.BodyHash, ttl.Microseconds(), "key-3").
		WillReturnResult(sqlmock.NewResult(0, 1))

	entry, created, err := store.Create(context.Background(), "key-3", fp, ttl)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true after takeover")
	}
	if entry == nil || entry.Token == "" {
		t.Fatalf("expected token in takeover entry, got %#v", entry)
	}

	assertExpectations(t, mock)
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta(store.selectQuery)).WithArgs("missing").WillReturnError(sql.ErrNoRows)

	entry, err := store.Get(context.Background(), "missing")
	if !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got entry=%#v err=%v", entry, err)
	}

	assertExpectations(t, mock)
}

func TestSetResponseNil(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newMockStore(t)
	defer cleanup()

	err := store.SetResponse(context.Background(), "key", "token", nil, time.Second)
	if !errors.Is(err, idempo.ErrResponseNil) {
		t.Fatalf("expected ErrResponseNil, got %v", err)
	}
}

func TestSetResponseExpired(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	resp := &idempo.Response{StatusCode: 200}
	ttl := 4 * time.Second

	mock.ExpectExec(regexp.QuoteMeta(store.commitQuery)).
		WithArgs(resp.StatusCode, false, sqlmock.AnyArg(), sqlmock.AnyArg(), ttl.Microseconds(), "key", "token").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.SetResponse(context.Background(), "key", "token", resp, ttl)
	if !errors.Is(err, idempo.ErrKeyExpired) {
		t.Fatalf("expected ErrKeyExpired, got %v", err)
	}

	assertExpectations(t, mock)
}

func TestDeleteExpired(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(store.deleteQuery)).
		WithArgs("key", "token").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.Delete(context.Background(), "key", "token")
	if !errors.Is(err, idempo.ErrKeyExpired) {
		t.Fatalf("expected ErrKeyExpired, got %v", err)
	}

	assertExpectations(t, mock)
}

func TestCleanupOnceLockNotAcquired(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	lockRows := sqlmock.NewRows([]string{"acquired"}).AddRow(0)
	mock.ExpectQuery(regexp.QuoteMeta(queryAcquireCleanupLock)).WithArgs(defaultCleanupLock).WillReturnRows(lockRows)

	store.cleanupOnce(context.Background())

	assertExpectations(t, mock)
}

func TestCleanupOnceLockAcquired(t *testing.T) {
	t.Parallel()

	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	lockRows := sqlmock.NewRows([]string{"acquired"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(queryAcquireCleanupLock)).WithArgs(defaultCleanupLock).WillReturnRows(lockRows)
	mock.ExpectExec(regexp.QuoteMeta(store.cleanupQuery)).WithArgs(defaultCleanupBatch).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta(queryReleaseCleanupLock)).WithArgs(defaultCleanupLock).WillReturnResult(sqlmock.NewResult(0, 0))

	store.cleanupOnce(context.Background())

	assertExpectations(t, mock)
}

func TestCleanupOnceReportsGetLockError(t *testing.T) {
	t.Parallel()

	reported := make(chan error, 1)
	store, mock, cleanup := newMockStore(t, WithCleanupErrorHandler(func(err error) { reported <- err }))
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta(queryAcquireCleanupLock)).WithArgs(defaultCleanupLock).WillReturnError(errors.New("boom"))

	store.cleanupOnce(context.Background())

	select {
	case err := <-reported:
		if err == nil {
			t.Fatalf("expected reported cleanup error")
		}
	case <-time.After(time.Second):
		t.Fatalf("expected cleanup error callback")
	}

	assertExpectations(t, mock)
}

func TestTTLValidation(t *testing.T) {
	t.Parallel()

	if _, err := ttlMicroseconds(0); err == nil {
		t.Fatalf("expected error for non-positive ttl")
	}
}

func TestQuoteTable(t *testing.T) {
	t.Parallel()

	quoted, err := quoteTable("schema_name.idempo_entries")
	if err != nil {
		t.Fatalf("quoteTable failed: %v", err)
	}
	if quoted != "`schema_name`.`idempo_entries`" {
		t.Fatalf("unexpected quoted table: %q", quoted)
	}

	if _, err := quoteTable("bad-name"); err == nil {
		t.Fatalf("expected invalid table name error")
	}
}

func TestIsDuplicateKey(t *testing.T) {
	t.Parallel()

	if !isDuplicateKey(&mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry"}) {
		t.Fatalf("expected duplicate key detection")
	}
	if isDuplicateKey(errors.New("other")) {
		t.Fatalf("unexpected duplicate key detection")
	}
}

func newMockStore(t *testing.T, opts ...Option) (*Store, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New failed: %v", err)
	}

	allOpts := append([]Option{WithTable(defaultTable)}, opts...)
	store, err := New(db, allOpts...)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
		_ = db.Close()
	}

	return store, mock, cleanup
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func selectColumns() []string {
	return []string{
		"token",
		"fp_operation",
		"fp_target",
		"fp_headers_hash",
		"fp_body_hash",
		"created_at",
		"updated_at",
		"resp_status_code",
		"resp_truncated",
		"resp_metadata",
		"resp_body",
	}
}
