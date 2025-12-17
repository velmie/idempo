package memory

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/velmie/idempo"
)

func TestMemoryStoreCreateGetSetDelete(t *testing.T) {
	t.Parallel()

	store := New()
	closeStore(t, store)
	fp := idempo.Fingerprint{Operation: "op", Target: "/t", BodyHash: "h"}

	entry, created, err := store.Create(context.Background(), "key", fp, time.Minute)
	if err != nil || !created || entry == nil {
		t.Fatalf("unexpected create result: entry=%#v created=%v err=%v", entry, created, err)
	}

	entry2, created, err := store.Create(context.Background(), "key", fp, time.Minute)
	if err != nil || created || entry2 == nil {
		t.Fatalf("expected existing entry: entry=%#v created=%v err=%v", entry2, created, err)
	}

	resp := &idempo.Response{StatusCode: 200, Body: []byte("body"), Metadata: map[string][]string{"K": {"V"}}}
	if err := store.SetResponse(context.Background(), "key", entry.Token, resp, time.Minute); err != nil {
		t.Fatalf("set response failed: %v", err)
	}

	got, err := store.Get(context.Background(), "key")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Response == nil || got.Response.StatusCode != resp.StatusCode || string(got.Response.Body) != "body" || got.Response.Metadata["K"][0] != "V" {
		t.Fatalf("stored response mismatch: %#v", got.Response)
	}

	if err := store.Delete(context.Background(), "key", entry.Token); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if got, err := store.Get(context.Background(), "key"); err == nil || !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound after delete, got entry=%#v err=%v", got, err)
	}
}

func TestMemoryStoreExpiration(t *testing.T) {
	t.Parallel()

	store := New()
	closeStore(t, store)
	fp := idempo.Fingerprint{Operation: "op", Target: "/t", BodyHash: "h"}

	_, created, err := store.Create(context.Background(), "exp", fp, -time.Second)
	if err != nil || !created {
		t.Fatalf("create failed: created=%v err=%v", created, err)
	}

	if got, err := store.Get(context.Background(), "exp"); err == nil || !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound for expired entry, got %#v err=%v", got, err)
	}
}

func TestMemoryStoreGetClonesResponse(t *testing.T) {
	t.Parallel()

	store := New()
	closeStore(t, store)

	resp := &idempo.Response{
		StatusCode: http.StatusOK,
		Body:       []byte("data"),
		Metadata:   map[string][]string{"H": {"v"}},
	}
	if err := store.SetResponse(context.Background(), "id", "", resp, time.Minute); err == nil {
		t.Fatalf("expected ErrKeyNotFound when setting before create, got nil")
	}
	fp := idempo.Fingerprint{}
	entry, _, err := store.Create(context.Background(), "id", fp, time.Minute)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := store.SetResponse(context.Background(), "id", entry.Token, resp, time.Minute); err != nil {
		t.Fatalf("set response failed: %v", err)
	}

	got, err := store.Get(context.Background(), "id")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if &got.Response.Body[0] == &resp.Body[0] {
		t.Fatalf("expected response body to be cloned")
	}
	if got.Response.Metadata["H"][0] != resp.Metadata["H"][0] {
		t.Fatalf("expected metadata copied")
	}
}

func closeStore(t *testing.T, store idempo.Store) {
	t.Helper()
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() {
			_ = closer.Close()
		})
	}
}
