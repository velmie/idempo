package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/velmie/idempo"
)

// Middleware wraps http.Handler with idempotency logic.
func Middleware(opts ...Option) func(http.Handler) http.Handler {
	c := NewConfig(opts...)
	if c.Engine == nil {
		panic("idempo/middleware: nil Engine")
	}
	methodSet := canonicalMethodSet(c.Methods)
	allowedHeaders := canonicalHeaderSet(c.AllowedResponseHeaders)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipMethod(methodSet, r.Method) {
				next.ServeHTTP(w, r)

				return
			}

			storeKey, hasKey, err := resolveStoreKey(r, c)
			if err != nil {
				c.ErrorHandler(w, r, err)

				return
			}
			if !hasKey {
				next.ServeHTTP(w, r)

				return
			}

			fp, err := buildFingerprint(r, c)
			if err != nil {
				c.ErrorHandler(w, r, err)

				return
			}
			result, err := c.Engine.Process(r.Context(), storeKey, fp)
			if err != nil {
				c.ErrorHandler(w, r, err)

				return
			}

			if result.Response != nil {
				markReplay(w, result.Response)

				return
			}

			if !result.IsOwner {
				c.ErrorHandler(w, r, idempo.ErrInProgress)

				return
			}

			handleFreshResponse(r.Context(), w, r, next, c, allowedHeaders, storeKey, result.Token)
		})
	}
}

func shouldSkipMethod(methodSet map[string]struct{}, method string) bool {
	if len(methodSet) == 0 {
		return false
	}

	_, ok := methodSet[strings.ToUpper(method)]

	return !ok
}

func resolveStoreKey(r *http.Request, c Config) (storeKey string, hasKey bool, err error) {
	rawKey := strings.TrimSpace(r.Header.Get(c.HeaderName))
	if rawKey == "" {
		if c.RequireKey {
			return "", false, idempo.ErrMissingKey
		}

		return "", false, nil
	}

	if err := c.KeyValidator(rawKey); err != nil {
		return "", false, err
	}

	return c.KeyPrefix + rawKey, true, nil
}

func markReplay(w http.ResponseWriter, resp *idempo.Response) {
	w.Header().Set("X-Idempotent-Replay", "true")
	writeHTTPResponse(w, resp)
}

func handleFreshResponse(
	parentCtx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	next http.Handler,
	cfg Config,
	allowedHeaders map[string]struct{},
	storeKey string,
	token string,
) {
	bw := newBufferingResponseWriter(w, cfg.MaxResponseBytes)
	defer bw.release()

	unlockOnReturn := true
	defer func() {
		if unlockOnReturn {
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), cfg.CommitTimeout)
			defer cancel()
			_ = cfg.Engine.Unlock(unlockCtx, storeKey, token)
		}
	}()

	next.ServeHTTP(bw, r)

	if !bw.stream && bw.body != nil && bw.body.Len() > 0 {
		bw.ensureContentType(bw.body.Bytes())
	}

	status := determineStatus(bw.status)
	respToStore := prepareStoredResponse(bw, status, cfg, allowedHeaders)

	if respToStore == nil {
		bw.flushTo(w, bw.overflow)

		return
	}

	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), cfg.CommitTimeout)
	defer cancel()
	if err := cfg.Engine.Commit(commitCtx, storeKey, token, respToStore); err != nil {
		if cfg.CommitErrorHandler != nil {
			cfg.CommitErrorHandler(r, err)
		}

		switch cfg.CommitErrorMode {
		case CommitFailOpen:
			bw.flushTo(w, bw.overflow)

			return
		case CommitFailClosedUnlock:
			if bw.stream {
				return
			}

			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		case CommitFailClosedKeepLock:
			unlockOnReturn = false
			if bw.stream {
				return
			}

			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		default:
			bw.flushTo(w, bw.overflow)

			return
		}
	}

	unlockOnReturn = false
	bw.flushTo(w, bw.overflow)
}

func determineStatus(status int) int {
	if status == 0 {
		return http.StatusOK
	}

	return status
}

func prepareStoredResponse(
	bw *bufferingResponseWriter,
	status int,
	cfg Config,
	allowedHeaders map[string]struct{},
) *idempo.Response {
	if !cfg.ShouldStore(status) {
		return nil
	}

	meta := filterHeaders(bw.headerForStore(), allowedHeaders, cfg.MaxStoredHeaderBytes)
	resp := &idempo.Response{
		StatusCode: status,
		Metadata:   meta,
		Body:       nil,
		Truncated:  bw.overflow,
	}
	if !bw.overflow {
		if bw.body != nil && bw.body.Len() > 0 {
			resp.Body = bw.body.Bytes()
		}
	}

	return resp
}

func canonicalMethodSet(methods []string) map[string]struct{} {
	methodSet := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		methodSet[strings.ToUpper(m)] = struct{}{}
	}

	return methodSet
}

func canonicalHeaderSet(headers []string) map[string]struct{} {
	allowedHeaders := make(map[string]struct{}, len(headers))
	for _, h := range headers {
		allowedHeaders[http.CanonicalHeaderKey(h)] = struct{}{}
	}

	return allowedHeaders
}

func buildFingerprint(r *http.Request, cfg Config) (idempo.Fingerprint, error) {
	if cfg.FingerprintFunc != nil {
		return cfg.FingerprintFunc(r)
	}
	shouldHash := cfg.ShouldHashBody(r)

	return fingerprintRequest(r, cfg, shouldHash)
}

var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},

	// Content-Length is message framing and must match the replayed body.
	"Content-Length": {},
}

const extraBlockedHeadersCapacity = 4

func filterHeaders(src http.Header, allowed map[string]struct{}, maxBytes int64) map[string][]string {
	if src == nil || len(allowed) == 0 {
		return map[string][]string{}
	}

	blocked := make(map[string]struct{}, len(hopByHopHeaders)+extraBlockedHeadersCapacity)
	for k := range hopByHopHeaders {
		blocked[k] = struct{}{}
	}
	for _, v := range src.Values("Connection") {
		for _, token := range strings.Split(v, ",") {
			norm := http.CanonicalHeaderKey(strings.TrimSpace(token))
			if norm == "" {
				continue
			}
			blocked[norm] = struct{}{}
		}
	}

	dst := make(map[string][]string, len(src))
	var used int64

	for k, vals := range src {
		norm := http.CanonicalHeaderKey(k)
		if _, ok := allowed[norm]; !ok {
			continue
		}
		if _, ok := blocked[norm]; ok {
			continue
		}
		for _, v := range vals {
			size := int64(len(norm) + len(v))
			if maxBytes > 0 && used+size > maxBytes {
				return dst
			}
			dst[norm] = append(dst[norm], v)
			used += size
		}
	}

	return dst
}
