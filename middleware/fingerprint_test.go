package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFingerprintRequestHonorsMaxBytes(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "http://example.com/path", strings.NewReader(strings.Repeat("a", int(defaultMaxPayloadBytes+1))))
	cfg := Config{MaxBodyBytes: defaultMaxPayloadBytes}.withDefaults()
	_, err := fingerprintRequest(req, cfg, true)
	if err == nil {
		t.Fatalf("expected ErrBodyTooLarge")
	}
}

func TestFingerprintRequestHashesBodyAndHeaders(t *testing.T) {
	t.Parallel()

	body := "payload"
	req := httptest.NewRequest(http.MethodPost, "http://example.com/path?q=1", strings.NewReader(body))
	req.Header.Set("X-Test", "value")

	cfg := Config{MaxBodyBytes: defaultMaxPayloadBytes, FingerprintHeaders: []string{"X-Test"}}.withDefaults()

	fp, err := fingerprintRequest(req, cfg, true)
	if err != nil {
		t.Fatalf("fingerprint failed: %v", err)
	}

	// body should be preserved for downstream reads
	data, _ := io.ReadAll(req.Body)
	if string(data) != body {
		t.Fatalf("body was not restored, got %q", string(data))
	}

	if fp.Operation != http.MethodPost || !strings.Contains(fp.BodyHash, "=") || !strings.Contains(fp.Target, "q=1") {
		t.Fatalf("unexpected fingerprint: %#v", fp)
	}
}

func TestFingerprintRequestSkipsBodyHash(t *testing.T) {
	t.Parallel()

	body := "payload"
	req := httptest.NewRequest(http.MethodPost, "http://example.com/path", strings.NewReader(body))

	cfg := Config{MaxBodyBytes: defaultMaxPayloadBytes}.withDefaults()
	fp, err := fingerprintRequest(req, cfg, false)
	if err != nil {
		t.Fatalf("fingerprint failed: %v", err)
	}

	data, _ := io.ReadAll(req.Body)
	if string(data) != body {
		t.Fatalf("body should remain unread when hashing disabled, got %q", string(data))
	}

	if fp.BodyHash == "" {
		t.Fatalf("hash should still be produced based on headers/path")
	}
}

func TestFingerprintRequestSortsQuery(t *testing.T) {
	t.Parallel()

	cfg := Config{}.withDefaults()

	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/path?b=2&a=1", nil)
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/path?a=1&b=2", nil)

	fp1, err := fingerprintRequest(req1, cfg, false)
	if err != nil {
		t.Fatalf("fingerprint failed: %v", err)
	}
	fp2, err := fingerprintRequest(req2, cfg, false)
	if err != nil {
		t.Fatalf("fingerprint failed: %v", err)
	}

	if fp1.Target != "/path?a=1&b=2" {
		t.Fatalf("expected sorted query, got %q", fp1.Target)
	}
	if fp1.Target != fp2.Target || fp1.BodyHash != fp2.BodyHash {
		t.Fatalf("expected identical fingerprints, got %#v vs %#v", fp1, fp2)
	}
}
