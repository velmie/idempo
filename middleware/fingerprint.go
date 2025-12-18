package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/velmie/idempo"
)

func fingerprintRequest(r *http.Request, cfg Config, shouldHashBody bool) (idempo.Fingerprint, error) {
	fp := idempo.Fingerprint{
		Operation: r.Method,
		Target:    r.URL.Path,
	}
	if r.URL.RawQuery != "" {
		fp.Target += "?" + sortQuery(r.URL.Query())
	}

	hdrHash := sha256.New()
	var hdrCount int
	for _, h := range cfg.FingerprintHeaders {
		vals := r.Header.Values(h)
		if len(vals) == 0 {
			continue
		}
		vals = append([]string(nil), vals...)
		sort.Strings(vals)
		for _, v := range vals {
			_, _ = hdrHash.Write([]byte(h))
			_, _ = hdrHash.Write([]byte{0})
			_, _ = hdrHash.Write([]byte(v))
			_, _ = hdrHash.Write([]byte{0})
			hdrCount++
		}
	}
	if hdrCount > 0 {
		fp.HeadersHash = base64.StdEncoding.EncodeToString(hdrHash.Sum(nil))
	}

	if !shouldHashBody || r.Body == nil {
		// still produce a stable hash to keep fingerprint shape consistent
		sum := sha256.Sum256(nil)
		fp.BodyHash = base64.StdEncoding.EncodeToString(sum[:])

		return fp, nil
	}

	limited := io.LimitReader(r.Body, cfg.MaxBodyBytes+1)
	body, err := io.ReadAll(limited)
	_ = r.Body.Close()
	if err != nil {
		return idempo.Fingerprint{}, fmt.Errorf("read body for fingerprint: %w", err)
	}
	if int64(len(body)) > cfg.MaxBodyBytes {
		return idempo.Fingerprint{}, idempo.ErrBodyTooLarge
	}

	// restore body for downstream handlers
	r.Body = io.NopCloser(bytes.NewReader(body))

	h := sha256.Sum256(body)
	fp.BodyHash = base64.StdEncoding.EncodeToString(h[:])

	return fp, nil
}

func sortQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	var parts []string
	for k, vals := range values {
		for _, v := range vals {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	sort.Strings(parts)

	return strings.Join(parts, "&")
}
