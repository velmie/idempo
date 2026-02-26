//go:build integration

package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	containermysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"github.com/velmie/idempo"
)

const createEntriesTableDDL = `
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
`

func TestIntegrationCreateCommitReplayDelete(t *testing.T) {
	ctx := context.Background()
	db := newIntegrationDB(t, ctx)

	store, err := New(db)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	fp := idempo.Fingerprint{Operation: "POST", Target: "/v1/orders", HeadersHash: "h1", BodyHash: "h2"}
	entry, created, err := store.Create(ctx, "order:1", fp, 2*time.Second)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}

	resp := &idempo.Response{
		StatusCode: 201,
		Metadata:   map[string][]string{"X-Req": {"r1"}},
		Body:       []byte("ok"),
	}
	if err := store.SetResponse(ctx, "order:1", entry.Token, resp, 2*time.Second); err != nil {
		t.Fatalf("SetResponse failed: %v", err)
	}

	got, err := store.Get(ctx, "order:1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Response == nil || got.Response.StatusCode != 201 || string(got.Response.Body) != "ok" {
		t.Fatalf("unexpected stored response: %#v", got.Response)
	}

	if err := store.Delete(ctx, "order:1", entry.Token); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := store.Get(ctx, "order:1"); !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound after delete, got %v", err)
	}
}

func TestIntegrationRejectsStaleOwnerAfterTakeover(t *testing.T) {
	ctx := context.Background()
	db := newIntegrationDB(t, ctx)

	store, err := New(db)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	fp := idempo.Fingerprint{Operation: "POST", Target: "/v1/payments", HeadersHash: "h1", BodyHash: "h2"}

	owner1, created, err := store.Create(ctx, "payment:1", fp, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	if !created {
		t.Fatalf("expected first create to own lock")
	}

	time.Sleep(120 * time.Millisecond)

	owner2, created, err := store.Create(ctx, "payment:1", fp, time.Second)
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if !created {
		t.Fatalf("expected takeover create to own lock")
	}
	if owner1.Token == owner2.Token {
		t.Fatalf("expected different tokens after takeover")
	}

	err = store.SetResponse(ctx, "payment:1", owner1.Token, &idempo.Response{StatusCode: 200}, time.Second)
	if !errors.Is(err, idempo.ErrKeyExpired) {
		t.Fatalf("expected stale owner commit rejected with ErrKeyExpired, got %v", err)
	}

	err = store.SetResponse(ctx, "payment:1", owner2.Token, &idempo.Response{StatusCode: 200}, time.Second)
	if err != nil {
		t.Fatalf("expected active owner commit success, got %v", err)
	}
}

func newIntegrationDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	container, err := containermysql.RunContainer(
		ctx,
		containermysql.WithDatabase("idempo"),
		containermysql.WithUsername("idempo"),
		containermysql.WithPassword("idempo"),
	)
	if err != nil {
		t.Fatalf("failed to start mysql container: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "parseTime=true", "loc=UTC", "multiStatements=true")
	if err != nil {
		t.Fatalf("failed to get mysql connection string: %v", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := waitForPing(ctx, db, 30*time.Second); err != nil {
		t.Fatalf("mysql ping failed: %v", err)
	}

	if _, err := db.ExecContext(ctx, createEntriesTableDDL); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

func waitForPing(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	dl := time.Now().Add(timeout)
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}
		if time.Now().After(dl) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for mysql ping")
}
