package idempo_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/velmie/idempo"
	"github.com/velmie/idempo/memory"
)

func TestProcessCommitStoresResponse(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store)

	reqCtx := context.Background()
	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/resource", BodyHash: "hash"}

	result, err := engine.Process(reqCtx, "key1", fp)
	if err != nil {
		t.Fatalf("process returned error: %v", err)
	}
	if result.Response != nil {
		t.Fatalf("expected fresh result, got replay")
	}
	if !result.IsOwner {
		t.Fatalf("expected lock owner, got owner=%v", result.IsOwner)
	}

	resp := &idempo.Response{StatusCode: http.StatusCreated, Metadata: map[string][]string{}, Body: []byte("ok")}
	if err := engine.Commit(context.Background(), "key1", result.Token, resp); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	entry, err := store.Get(context.Background(), "key1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if entry == nil || entry.Response == nil {
		t.Fatalf("stored response missing or invalid: %#v", entry)
	}
}

func TestCommitRejectsNilResponse(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store)

	if err := engine.Commit(context.Background(), "key-nil", "ignored", nil); !errors.Is(err, idempo.ErrResponseNil) {
		t.Fatalf("expected ErrResponseNil, got %v", err)
	}
}

func TestUnlockDeletes(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store)

	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/delete", BodyHash: "hash"}
	result, err := engine.Process(context.Background(), "key2", fp)
	if err != nil {
		t.Fatalf("process returned error: %v", err)
	}
	if result.Response != nil {
		t.Fatalf("expected fresh operation, got replay")
	}
	if !result.IsOwner {
		t.Fatalf("expected lock owner, got owner=%v", result.IsOwner)
	}
	if err := engine.Unlock(context.Background(), "key2", result.Token); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	if entry, err := store.Get(context.Background(), "key2"); err == nil {
		t.Fatalf("expected ErrKeyNotFound after unlock, got %#v", entry)
	}
}

func TestProcessReplayAndConflict(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store)

	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/item", BodyHash: "hash"}
	fpOther := idempo.Fingerprint{Operation: http.MethodPost, Target: "/item", BodyHash: "other"}

	entry, created, err := store.Create(context.Background(), "key3", fp, time.Minute)
	if err != nil || !created {
		t.Fatalf("create failed: created=%v err=%v", created, err)
	}
	if err := store.SetResponse(context.Background(), "key3", entry.Token, &idempo.Response{StatusCode: http.StatusOK, Metadata: map[string][]string{}}, time.Minute); err != nil {
		t.Fatalf("set response failed: %v", err)
	}

	result, err := engine.Process(context.Background(), "key3", fp)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if result.Response == nil || result.IsOwner {
		t.Fatalf("expected replay response")
	}

	result, err = engine.Process(context.Background(), "key3", fpOther)
	if err == nil || !errors.Is(err, idempo.ErrKeyConflict) {
		t.Fatalf("expected ErrKeyConflict, got %v", err)
	}
	if result.IsOwner {
		t.Fatalf("expected owner=false on conflict")
	}
}

func TestProcessWaitsForInProgress(t *testing.T) {
	t.Parallel()

	baseStore := memory.New()
	store := newSignalingStore(baseStore)
	closeStore(t, store)
	engine := idempo.NewEngine(store, idempo.WithWaitForInProgress(true), idempo.WithPollInterval(5*time.Millisecond))
	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/wait", BodyHash: "hash"}

	allowCommit := make(chan struct{})
	ownerErr := make(chan error, 1)
	go func() {
		ctx := context.Background()
		entry, created, err := store.Create(ctx, "key4", fp, time.Minute)
		if err != nil {
			ownerErr <- fmt.Errorf("create failed: %w", err)

			return
		}
		if !created {
			ownerErr <- errors.New("expected new lock")

			return
		}

		<-allowCommit

		err = store.SetResponse(ctx, "key4", entry.Token, &idempo.Response{
			StatusCode: http.StatusAccepted,
			Body:       []byte("done"),
			Metadata:   map[string][]string{},
		}, time.Minute)
		ownerErr <- err
	}()

	select {
	case <-store.locked:
	case err := <-ownerErr:
		t.Fatalf("owner goroutine failed early: %v", err)
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for lock acquisition")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	replayCh := make(chan *idempo.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := engine.Process(ctx, "key4", fp)
		if err != nil {
			errCh <- err

			return
		}
		if result.IsOwner {
			errCh <- errors.New("expected waiter to be non-owner")

			return
		}
		replayCh <- result.Response
	}()

	close(allowCommit)

	select {
	case <-store.committed:
	case err := <-ownerErr:
		t.Fatalf("owner goroutine failed to commit: %v", err)
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for owner commit")
	}

	select {
	case err := <-errCh:
		t.Fatalf("process failed: %v", err)
	case replay := <-replayCh:
		if replay == nil || string(replay.Body) != "done" {
			t.Fatalf("unexpected wait replay result: %#v", replay)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for replay response")
	}
}

func TestProcessWaitTimeout(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store, idempo.WithWaitForInProgress(true), idempo.WithInProgressTimeout(20*time.Millisecond), idempo.WithPollInterval(5*time.Millisecond))
	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/timeout", BodyHash: "hash"}

	_, created, err := store.Create(context.Background(), "key5", fp, time.Minute)
	if err != nil || !created {
		t.Fatalf("create failed: created=%v err=%v", created, err)
	}

	result, err := engine.Process(context.Background(), "key5", fp)
	if err == nil || !errors.Is(err, idempo.ErrWaitTimeout) {
		t.Fatalf("expected wait timeout, got %v", err)
	}
	if result.IsOwner {
		t.Fatalf("expected owner=false on wait timeout")
	}
	if result.Response != nil {
		t.Fatalf("expected nil replay on wait timeout")
	}
}

func TestProcessWaitKeyDisappears(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store, idempo.WithWaitForInProgress(true), idempo.WithInProgressTimeout(200*time.Millisecond), idempo.WithPollInterval(5*time.Millisecond))
	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/vanish", BodyHash: "hash"}

	entry, created, err := store.Create(context.Background(), "key-missing", fp, time.Minute)
	if err != nil || !created {
		t.Fatalf("create failed: created=%v err=%v", created, err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = store.Delete(context.Background(), "key-missing", entry.Token)
	}()

	result, err := engine.Process(context.Background(), "key-missing", fp)
	if err == nil || !errors.Is(err, idempo.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound after key disappeared, got %v", err)
	}
	if result != (idempo.Result{}) {
		t.Fatalf("expected nil result on missing key, got %#v", result)
	}
}

func TestProcessWaitFingerprintChanges(t *testing.T) {
	t.Parallel()

	store := memory.New()
	closeStore(t, store)
	engine := idempo.NewEngine(store, idempo.WithWaitForInProgress(true), idempo.WithInProgressTimeout(200*time.Millisecond), idempo.WithPollInterval(5*time.Millisecond))
	fp := idempo.Fingerprint{Operation: http.MethodPost, Target: "/stable", BodyHash: "hash"}
	fpOther := idempo.Fingerprint{Operation: http.MethodPost, Target: "/stable", BodyHash: "other"}

	entry, created, err := store.Create(context.Background(), "key-change", fp, time.Minute)
	if err != nil || !created {
		t.Fatalf("create failed: created=%v err=%v", created, err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = store.Delete(context.Background(), "key-change", entry.Token)
		_, _, _ = store.Create(context.Background(), "key-change", fpOther, time.Minute)
	}()

	result, err := engine.Process(context.Background(), "key-change", fp)
	if err == nil || !errors.Is(err, idempo.ErrKeyConflict) {
		t.Fatalf("expected ErrKeyConflict after fingerprint change, got %v", err)
	}
	if result != (idempo.Result{}) {
		t.Fatalf("expected nil result on fingerprint change, got %#v", result)
	}
}

type signalingStore struct {
	idempo.Store
	locked    chan struct{}
	committed chan struct{}
}

func newSignalingStore(base idempo.Store) *signalingStore {
	return &signalingStore{
		Store:     base,
		locked:    make(chan struct{}, 1),
		committed: make(chan struct{}, 1),
	}
}

func (s *signalingStore) Close() error {
	if closer, ok := s.Store.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}

func (s *signalingStore) notifyLocked() {
	select {
	case s.locked <- struct{}{}:
	default:
	}
}

func (s *signalingStore) notifyCommitted() {
	select {
	case s.committed <- struct{}{}:
	default:
	}
}

func (s *signalingStore) Create(ctx context.Context, key string, fp idempo.Fingerprint, ttl time.Duration) (*idempo.Entry, bool, error) {
	entry, created, err := s.Store.Create(ctx, key, fp, ttl)
	if created {
		s.notifyLocked()
	}

	return entry, created, err
}

func (s *signalingStore) SetResponse(ctx context.Context, key, token string, resp *idempo.Response, ttl time.Duration) error {
	err := s.Store.SetResponse(ctx, key, token, resp, ttl)
	if err == nil {
		s.notifyCommitted()
	}

	return err
}

func closeStore(t *testing.T, store idempo.Store) {
	t.Helper()
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() {
			_ = closer.Close()
		})
	}
}
