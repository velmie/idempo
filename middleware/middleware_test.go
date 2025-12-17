package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/velmie/idempo"
	"github.com/velmie/idempo/memory"
)

func TestMiddlewareStoresAndReplaysResponse(t *testing.T) {
	t.Parallel()

	store := memory.New()
	t.Cleanup(func() { _ = store.Close() })
	engine := idempo.NewEngine(store)
	mw := Middleware(
		WithEngine(engine),
		WithAllowedResponseHeaders("X-Test"),
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com", strings.NewReader("body"))
	req.Header.Set("Idempotency-Key", "id1")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "hello" {
		t.Fatalf("expected handler response, got %q", rec.Body.String())
	}
	if entry, _ := store.Get(context.Background(), "id1"); entry == nil || entry.Response == nil {
		t.Fatalf("response not stored after first call")
	}

	// second request should be replayed
	req2 := httptest.NewRequest(http.MethodPost, "http://example.com", strings.NewReader("body"))
	req2.Header.Set("Idempotency-Key", "id1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatalf("replay header missing")
	}
	if rec2.Body.String() != "hello" {
		t.Fatalf("expected replayed body, got %q", rec2.Body.String())
	}
	if rec2.Header().Get("X-Test") != "yes" {
		t.Fatalf("metadata was not stored")
	}
}

func TestMiddlewareRequireKey(t *testing.T) {
	t.Parallel()

	store := memory.New()
	t.Cleanup(func() { _ = store.Close() })
	engine := idempo.NewEngine(store)
	mw := Middleware(
		WithEngine(engine),
		WithRequireKey(true),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request when key missing, got %d", rec.Code)
	}
}

func TestMiddlewareAllowsTruncatedResponses(t *testing.T) {
	t.Parallel()

	store := memory.New()
	t.Cleanup(func() { _ = store.Close() })
	engine := idempo.NewEngine(store)
	mw := Middleware(
		WithEngine(engine),
		WithMaxResponseBytes(4),
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set("Idempotency-Key", "too-big")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK on truncation, got %d", rec.Code)
	}
	if rec.Body.String() != "12345" {
		t.Fatalf("expected full response body, got %q", rec.Body.String())
	}
	if rec.Header().Get("X-Idempotent-Truncated") != "true" {
		t.Fatalf("expected truncated response header")
	}

	entry, _ := store.Get(context.Background(), "too-big")
	if entry == nil || entry.Response == nil {
		t.Fatalf("expected stored truncated response, got %#v", entry)
	}
	if entry.Response.Truncated != true {
		t.Fatalf("expected stored response marked truncated")
	}
	if len(entry.Response.Body) != 0 {
		t.Fatalf("expected truncated response body not to be stored")
	}
}

type failingCommitStore struct {
	idempo.Store
	err error
}

func (s *failingCommitStore) SetResponse(ctx context.Context, key, token string, resp *idempo.Response, ttl time.Duration) error {
	return s.err
}

func TestMiddlewareCommitFailOpenUnlocksKey(t *testing.T) {
	t.Parallel()

	base := memory.New()
	t.Cleanup(func() { _ = base.Close() })
	engine := idempo.NewEngine(&failingCommitStore{Store: base, err: errors.New("commit failed")})
	mw := Middleware(WithEngine(engine))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set("Idempotency-Key", "id-commit-fail-open")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("expected handler response on commit failure, got status=%d body=%q", rec.Code, rec.Body.String())
	}
	if _, err := base.Get(context.Background(), "id-commit-fail-open"); !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected key to be unlocked on commit failure, got %v", err)
	}
}

func TestMiddlewareCommitFailClosedKeepsLock(t *testing.T) {
	t.Parallel()

	base := memory.New()
	t.Cleanup(func() { _ = base.Close() })
	engine := idempo.NewEngine(&failingCommitStore{Store: base, err: errors.New("commit failed")})
	mw := Middleware(
		WithEngine(engine),
		WithCommitErrorMode(CommitFailClosedKeepLock),
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set("Idempotency-Key", "id-commit-fail-closed")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on commit failure, got %d", rec.Code)
	}
	if _, err := base.Get(context.Background(), "id-commit-fail-closed"); err != nil {
		t.Fatalf("expected key to remain locked on commit failure, got %v", err)
	}
}

func TestMiddlewareDoesNotReplayHopByHopHeaders(t *testing.T) {
	t.Parallel()

	store := memory.New()
	t.Cleanup(func() { _ = store.Close() })
	engine := idempo.NewEngine(store)
	mw := Middleware(
		WithEngine(engine),
		WithAllowedResponseHeaders("X-Ok", "X-Hop", "Transfer-Encoding", "Upgrade", "Connection"),
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ok", "yes")
		w.Header().Set("X-Hop", "no")
		w.Header().Set("Connection", "X-Hop, Upgrade")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("Upgrade", "websocket")
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set("Idempotency-Key", "id-hop")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	req2 := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req2.Header.Set("Idempotency-Key", "id-hop")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Ok") != "yes" {
		t.Fatalf("expected X-Ok to be replayed, got %q", rec2.Header().Get("X-Ok"))
	}
	if rec2.Header().Get("X-Hop") != "" {
		t.Fatalf("expected X-Hop to be excluded, got %q", rec2.Header().Get("X-Hop"))
	}
	if rec2.Header().Get("Transfer-Encoding") != "" {
		t.Fatalf("expected Transfer-Encoding to be excluded, got %q", rec2.Header().Get("Transfer-Encoding"))
	}
	if rec2.Header().Get("Upgrade") != "" {
		t.Fatalf("expected Upgrade to be excluded, got %q", rec2.Header().Get("Upgrade"))
	}
	if rec2.Header().Get("Connection") != "" {
		t.Fatalf("expected Connection to be excluded, got %q", rec2.Header().Get("Connection"))
	}
}
