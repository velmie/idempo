package redis

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/velmie/idempo"
)

// Store implements idempo.Store backed by Redis.
type Store struct {
	client redisv9.UniversalClient
	opts   Options
}

// New constructs Store around any go-redis compatible client.
func New(client redisv9.UniversalClient, opts ...Option) *Store {
	if client == nil {
		panic("idempo/redis: nil client")
	}

	cfg := Options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg.withDefaults()

	return &Store{
		client: client,
		opts:   cfg,
	}
}

var _ idempo.Store = (*Store)(nil)

var (
	errUnexpectedCreateResult = errors.New("unexpected create script result")
	errUnexpectedCreateFlag   = errors.New("unexpected create script flag")
	errUnexpectedCommitResult = errors.New("unexpected commit script result")
	errUnexpectedBulkType     = errors.New("unexpected bulk type")
)

func (s *Store) Create(
	ctx context.Context,
	key string,
	fp idempo.Fingerprint,
	ttl time.Duration,
) (*idempo.Entry, bool, error) {
	ttlMs, err := ttlMilliseconds(ttl)
	if err != nil {
		return nil, false, err
	}

	redisKey := s.redisKey(key)
	now := s.opts.Now()

	token, err := newToken()
	if err != nil {
		return nil, false, err
	}

	entry := &idempo.Entry{
		Key:         key,
		Fingerprint: fp,
		Token:       token,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	payload, err := s.opts.Codec.MarshalEntry(entry)
	if err != nil {
		return nil, false, fmt.Errorf("marshal redis entry: %w", err)
	}

	encodedValue := encodeValue(token, payload)
	rawResult, err := createScript.Run(ctx, s.client, []string{redisKey}, encodedValue, ttlMs).Result()
	if err != nil {
		return nil, false, fmt.Errorf("redis create script: %w", err)
	}
	parts, ok := rawResult.([]interface{})
	if !ok || len(parts) != 2 {
		return nil, false, fmt.Errorf("%w: %T %v", errUnexpectedCreateResult, rawResult, rawResult)
	}

	createdFlag, err := parseInt(parts[0], errUnexpectedCreateFlag)
	if err != nil {
		return nil, false, err
	}

	value, err := decodeBulk(parts[1])
	if err != nil {
		return nil, false, fmt.Errorf("decode create script payload: %w", err)
	}

	storedToken, storedPayload, err := splitValue(value)
	if err != nil {
		return nil, false, fmt.Errorf("decode redis entry: %w", err)
	}

	stored, err := s.opts.Codec.UnmarshalEntry(storedPayload)
	if err != nil {
		return nil, false, fmt.Errorf("decode redis entry: %w", err)
	}
	stored.Token = storedToken
	if stored.Key == "" {
		stored.Key = key
	}

	return stored, createdFlag == 1, nil
}

func (s *Store) Get(ctx context.Context, key string) (*idempo.Entry, error) {
	entry, err := s.loadEntry(ctx, key)
	if err != nil {
		return nil, err
	}

	return entry, nil
}

func (s *Store) SetResponse(
	ctx context.Context,
	key, token string,
	resp *idempo.Response,
	ttl time.Duration,
) error {
	ttlMs, err := ttlMilliseconds(ttl)
	if err != nil {
		return err
	}

	if resp == nil {
		return idempo.ErrResponseNil
	}

	entry, err := s.loadEntry(ctx, key)
	if err != nil {
		if errors.Is(err, idempo.ErrKeyNotFound) {
			return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
		}

		return err
	}

	entry.Response = idempo.CloneResponse(resp)
	entry.Token = token
	entry.UpdatedAt = s.opts.Now()

	payload, err := s.opts.Codec.MarshalEntry(entry)
	if err != nil {
		return fmt.Errorf("marshal redis entry: %w", err)
	}

	redisKey := s.redisKey(key)
	value := encodeValue(token, payload)
	res, err := commitScript.Run(ctx, s.client, []string{redisKey}, token, value, ttlMs).Result()
	if err != nil {
		return fmt.Errorf("redis commit script: %w", err)
	}

	updated, err := parseInt(res, errUnexpectedCommitResult)
	if err != nil {
		return err
	}
	switch updated {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	case -1:
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	default:
		return fmt.Errorf("%w: %d", errUnexpectedCommitResult, updated)
	}
}

func (s *Store) Delete(ctx context.Context, key, token string) error {
	redisKey := s.redisKey(key)
	res, err := deleteScript.Run(ctx, s.client, []string{redisKey}, token).Result()
	if err != nil {
		return fmt.Errorf("redis del: %w", err)
	}

	updated, err := parseInt(res, errUnexpectedCommitResult)
	if err != nil {
		return err
	}
	switch updated {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	case -1:
		return fmt.Errorf("%w: %s", idempo.ErrKeyExpired, key)
	default:
		return fmt.Errorf("%w: %d", errUnexpectedCommitResult, updated)
	}
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) redisKey(raw string) string {
	if s.opts.KeyPrefix == "" {
		return raw
	}

	return s.opts.KeyPrefix + ":" + raw
}

func decodeBulk(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	default:
		return nil, fmt.Errorf("%w: %T", errUnexpectedBulkType, v)
	}
}

func parseInt(v interface{}, errType error) (int64, error) {
	switch val := v.(type) {
	case int64:
		return val, nil
	case int:
		return int64(val), nil
	case float64:
		return int64(val), nil
	default:
		return 0, fmt.Errorf("%w: %T", errType, v)
	}
}

func (s *Store) loadEntry(ctx context.Context, key string) (*idempo.Entry, error) {
	redisKey := s.redisKey(key)

	raw, err := s.client.Get(ctx, redisKey).Bytes()
	if errors.Is(err, redisv9.Nil) {
		return nil, idempo.ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}

	token, payload, err := splitValue(raw)
	if err != nil {
		return nil, fmt.Errorf("decode redis entry: %w", err)
	}

	entry, err := s.opts.Codec.UnmarshalEntry(payload)
	if err != nil {
		return nil, fmt.Errorf("decode redis entry: %w", err)
	}
	if entry.Key == "" {
		entry.Key = key
	}
	entry.Token = token

	return entry, nil
}

func ttlMilliseconds(ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, fmt.Errorf("idempo/redis: invalid ttl: %v", ttl)
	}

	ms := ttl.Milliseconds()
	if ms <= 0 {
		ms = 1
	}

	return ms, nil
}

func newToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("idempo/redis: generate token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func encodeValue(token string, payload []byte) []byte {
	dst := make([]byte, 0, len(token)+1+len(payload))
	dst = append(dst, token...)
	dst = append(dst, '\n')
	dst = append(dst, payload...)

	return dst
}

func splitValue(raw []byte) (token string, payload []byte, err error) {
	if len(raw) == 0 {
		return "", nil, errors.New("empty payload")
	}

	nl := bytes.IndexByte(raw, '\n')
	if nl < 0 {
		// Backwards compatibility: old versions stored JSON directly without token prefix.
		return "", raw, nil
	}

	return string(raw[:nl]), raw[nl+1:], nil
}
