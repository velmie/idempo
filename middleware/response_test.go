package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/velmie/idempo"
)

func TestWriteHTTPResponseHandlesNil(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeHTTPResponse(rec, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected internal error for nil response, got %d", rec.Code)
	}
}

func TestWriteHTTPResponseWritesAll(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	resp := &idempo.Response{
		StatusCode: http.StatusAccepted,
		Body:       []byte("ok"),
		Metadata: map[string][]string{
			"X-Test": {"y"},
		},
	}
	writeHTTPResponse(rec, resp)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status accepted, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected body written")
	}
	if rec.Header().Get("X-Test") != "y" {
		t.Fatalf("expected metadata headers")
	}
}

func TestWriteHTTPResponseTruncated(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	resp := &idempo.Response{
		StatusCode: http.StatusOK,
		Metadata:   map[string][]string{},
		Truncated:  true,
	}
	writeHTTPResponse(rec, resp)

	if rec.Header().Get("X-Idempotent-Truncated") != "true" {
		t.Fatalf("expected truncated header")
	}
}
