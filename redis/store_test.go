package redis

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/velmie/idempo"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: mr.Addr()})
	store := New(client)

	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})

	return store, mr
}

func TestCreateAndGet(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := context.Background()
	fp := idempo.Fingerprint{Operation: "POST", Target: "/v1/resource", BodyHash: "hash"}

	entry, created, err := store.Create(ctx, "key-1", fp, time.Minute)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created flag true")
	}
	if entry.Fingerprint != fp {
		t.Fatalf("fingerprint mismatch: %+v", entry.Fingerprint)
	}
	if entry.Response != nil {
		t.Fatalf("expected nil response for new entry")
	}

	entry2, created, err := store.Create(ctx, "key-1", fp, time.Minute)
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if created {
		t.Fatalf("expected created flag false on duplicate")
	}
	if !entry.CreatedAt.Equal(entry2.CreatedAt) {
		t.Fatalf("createdAt mismatch after duplicate create")
	}

	got, err := store.Get(ctx, "key-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got == nil || got.Fingerprint != fp {
		t.Fatalf("unexpected entry returned: %+v", got)
	}
}

func TestSetResponseUpdatesEntry(t *testing.T) {
	t.Parallel()

	store, mr := newTestStore(t)
	ctx := context.Background()
	key := "resp-key"
	fp := idempo.Fingerprint{Operation: "PUT", Target: "/v1/resource/1", BodyHash: "body-hash"}

	entry, _, err := store.Create(ctx, key, fp, time.Minute)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	resp := &idempo.Response{
		StatusCode: http.StatusAccepted,
		Metadata:   map[string][]string{"H": {"v1", "v2"}},
		Body:       []byte("payload"),
		Truncated:  true,
	}
	if err := store.SetResponse(ctx, key, entry.Token, resp, 2*time.Second); err != nil {
		t.Fatalf("set response failed: %v", err)
	}

	entry, err = store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if entry == nil || entry.Response == nil {
		t.Fatalf("expected stored response")
	}
	if entry.Response.StatusCode != resp.StatusCode || string(entry.Response.Body) != string(resp.Body) {
		t.Fatalf("stored response mismatch: %+v", entry.Response)
	}
	if entry.Response.Truncated != resp.Truncated {
		t.Fatalf("truncated flag mismatch")
	}

	redisKey := defaultKeyPrefix + ":" + key
	ttl := mr.TTL(redisKey)
	if ttl <= 0 {
		t.Fatalf("expected ttl to be set")
	}
	if ttl > 2*time.Second || ttl < time.Second {
		t.Fatalf("unexpected ttl: %v", ttl)
	}
}

func TestSetResponseNil(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := context.Background()
	key := "nil-response"

	entry, _, err := store.Create(ctx, key, idempo.Fingerprint{}, time.Minute)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := store.SetResponse(ctx, key, entry.Token, nil, time.Minute); !errors.Is(err, idempo.ErrResponseNil) {
		t.Fatalf("expected ErrResponseNil, got %v", err)
	}
}

func TestSetResponseExpired(t *testing.T) {
	t.Parallel()

	store, mr := newTestStore(t)
	ctx := context.Background()
	key := "expire-key"

	entry, _, err := store.Create(ctx, key, idempo.Fingerprint{}, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	mr.FastForward(50 * time.Millisecond)

	err = store.SetResponse(ctx, key, entry.Token, &idempo.Response{StatusCode: http.StatusOK}, time.Minute)
	if !errors.Is(err, idempo.ErrKeyExpired) {
		t.Fatalf("expected ErrKeyExpired, got %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := context.Background()

	if entry, err := store.Get(ctx, "missing"); err == nil || !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got entry=%#v err=%v", entry, err)
	}
}

func TestCustomKeyPrefix(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: mr.Addr()})
	store := New(client, WithKeyPrefix("custom"))

	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})

	ctx := context.Background()
	if _, _, err := store.Create(ctx, "id", idempo.Fingerprint{}, time.Minute); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if !mr.Exists("custom:id") {
		t.Fatalf("expected key with custom prefix to be stored")
	}
}
