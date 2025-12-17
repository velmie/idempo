package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/velmie/idempo"
	"github.com/velmie/idempo/middleware"
	"github.com/velmie/idempo/memory"
)

func ExampleMiddleware() {
	store := memory.New()
	defer func() { _ = store.Close() }()

	engine := idempo.NewEngine(store)
	mw := middleware.Middleware(middleware.WithEngine(engine), middleware.WithRequireKey(true))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	req1 := httptest.NewRequest(http.MethodPost, "http://example.com/resource", strings.NewReader("body"))
	req1.Header.Set("Idempotency-Key", "k1")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "http://example.com/resource", strings.NewReader("body"))
	req2.Header.Set("Idempotency-Key", "k1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	fmt.Println(rec2.Header().Get("X-Idempotent-Replay"), rec2.Body.String())

	// Output: true hello
}

