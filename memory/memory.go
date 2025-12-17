package memory

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/velmie/idempo"
)

const defaultCleanupInterval = 5 * time.Minute

// Store is an in-memory idempotency store.
// It starts a cleanup goroutine; call Close when done.
type Store struct {
	mu              sync.RWMutex
	items           map[string]*memItem
	cleanupInterval time.Duration
	done            chan struct{}
	closeOnce       sync.Once
}

type memItem struct {
	entry     idempo.Entry
	expiresAt time.Time
}

var _ idempo.Store = (*Store)(nil)

// New returns an in-memory Store implementation intended for tests and local development.
// Call Close to stop the background cleanup loop when the store is no longer needed.
func New() *Store {
	s := &Store{
		items:           make(map[string]*memItem),
		cleanupInterval: defaultCleanupInterval,
		done:            make(chan struct{}),
	}

	go s.cleanupLoop()

	return s
}

func (s *Store) Create(
	_ context.Context,
	key string,
	fp idempo.Fingerprint,
	ttl time.Duration,
) (entry *idempo.Entry, created bool, err error) {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if item, ok := s.items[key]; ok {
		if now.After(item.expiresAt) {
			delete(s.items, key)
		} else {
			return cloneEntry(&item.entry), false, nil
		}
	}

	token, err := newToken()
	if err != nil {
		return nil, false, err
	}

	entryVal := idempo.Entry{
		Key:         key,
		Fingerprint: fp,
		Token:       token,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.items[key] = &memItem{
		entry:     entryVal,
		expiresAt: now.Add(ttl),
	}

	return cloneEntry(&entryVal), true, nil
}

func (s *Store) Get(_ context.Context, key string) (entry *idempo.Entry, err error) {
	now := time.Now()

	s.mu.RLock()
	item, ok := s.items[key]
	if !ok {
		s.mu.RUnlock()

		return nil, idempo.ErrKeyNotFound
	}

	expired := now.After(item.expiresAt)
	entry = cloneEntry(&item.entry)
	s.mu.RUnlock()

	if expired {
		s.mu.Lock()
		if cur, ok := s.items[key]; ok && now.After(cur.expiresAt) {
			delete(s.items, key)
		}
		s.mu.Unlock()

		return nil, idempo.ErrKeyNotFound
	}

	return entry, nil
}

func (s *Store) SetResponse(_ context.Context, key, token string, resp *idempo.Response, ttl time.Duration) (err error) {
	if resp == nil {
		return idempo.ErrResponseNil
	}

	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[key]
	if !ok {
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	}
	if now.After(item.expiresAt) {
		delete(s.items, key)

		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	}
	if item.entry.Token != token {
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	}

	item.entry.Response = idempo.CloneResponse(resp)
	item.entry.UpdatedAt = now
	item.expiresAt = now.Add(ttl)

	return nil
}

func (s *Store) Delete(_ context.Context, key, token string) (err error) {
	s.mu.Lock()
	if item, ok := s.items[key]; ok && item.entry.Token == token {
		delete(s.items, key)
	}
	s.mu.Unlock()

	return nil
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup(time.Now())
		case <-s.done:
			return
		}
	}
}

func (s *Store) cleanup(now time.Time) {
	s.mu.Lock()
	for key, item := range s.items {
		if now.After(item.expiresAt) {
			delete(s.items, key)
		}
	}
	s.mu.Unlock()
}

func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
	})

	return nil
}

func cloneEntry(src *idempo.Entry) *idempo.Entry {
	dst := *src
	if src.Response != nil {
		dst.Response = idempo.CloneResponse(src.Response)
	}

	return &dst
}

func newToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("idempo/memory: generate token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
