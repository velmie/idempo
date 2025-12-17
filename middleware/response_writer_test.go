package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBufferingResponseWriterStoresUntilFlush(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	bw := newBufferingResponseWriter(rec, defaultMaxPayloadBytes)
	defer bw.release()

	bw.Header().Set("X-Test", "1")
	bw.WriteHeader(http.StatusCreated)
	_, _ = bw.Write([]byte("hello"))
	bw.flushTo(rec, false)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("expected body to be written, got %q", rec.Body.String())
	}
	if rec.Header().Get("X-Test") != "1" {
		t.Fatalf("expected header passthrough")
	}
}

func TestBufferingResponseWriterOverflow(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	bw := newBufferingResponseWriter(rec, 4)
	defer bw.release()

	_, _ = bw.Write([]byte("12"))
	_, _ = bw.Write([]byte("3456"))
	bw.flushTo(rec, bw.overflow)

	if rec.Body.String() != "123456" {
		t.Fatalf("overflow should flush buffered data to client, got %q", rec.Body.String())
	}
	if rec.Header().Get("X-Idempotent-Truncated") != "true" {
		t.Fatalf("expected truncated header")
	}
}
