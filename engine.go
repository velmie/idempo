package idempo

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

const (
	defaultLockTTL           = time.Minute
	defaultResultTTL         = 24 * time.Hour
	defaultInProgressTimeout = 30 * time.Second
	defaultPollInterval      = 50 * time.Millisecond
)

// Config defines behavior of the Engine.
type Config struct {
	// LockTTL limits how long an in-flight operation holds a lock before a response is stored.
	LockTTL time.Duration
	// ResultTTL controls how long a completed response is kept.
	ResultTTL time.Duration
	// WaitForInProgress toggles waiting for another in-progress operation with the same key.
	WaitForInProgress bool
	// InProgressTimeout caps waiting time for another in-flight operation.
	InProgressTimeout time.Duration
	// PollInterval defines how often to poll the store while waiting for a response.
	PollInterval time.Duration
}

func (c Config) withDefaults() Config {
	if c.LockTTL <= 0 {
		c.LockTTL = defaultLockTTL
	}
	if c.ResultTTL <= 0 {
		c.ResultTTL = defaultResultTTL
	}
	if c.InProgressTimeout <= 0 {
		c.InProgressTimeout = defaultInProgressTimeout
	}
	if c.PollInterval <= 0 {
		c.PollInterval = defaultPollInterval
	}

	return c
}

// Engine orchestrates idempotent execution using provided Store.
type Engine struct {
	store Store
	cfg   Config

	rngMu sync.Mutex
	rng   *rand.Rand
}

// Result describes the outcome of Engine.Process.
// When IsOwner is true, the caller acquired the lock and must Commit or Unlock using the returned Token.
// When Response is non-nil, the caller should replay it instead of performing the operation.
type Result struct {
	Response *Response
	IsOwner  bool
	// Token is a per-lock opaque lease identifier.
	// It is set only when IsOwner is true and must be passed to Commit/Unlock.
	Token string
}

// NewEngine constructs Engine with defaults and optional options.
// Some Store implementations may require cleanup when no longer used.
func NewEngine(store Store, opts ...EngineOption) *Engine {
	if store == nil {
		panic("idempo: nil Store")
	}

	var cfg Config
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg = cfg.withDefaults()

	return &Engine{
		store: store,
		cfg:   cfg,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Process registers or replays an idempotent operation.
// Result.Response is non-nil when a stored result should be replayed.
// When IsOwner is true, the caller holds the lock and must finish with Commit (or Unlock on failure) using Result.Token.
func (e *Engine) Process(
	ctx context.Context,
	key string,
	fp Fingerprint,
) (Result, error) {
	entry, created, err := e.store.Create(ctx, key, fp, e.cfg.LockTTL)
	if err != nil {
		return Result{}, fmt.Errorf("idempotency store create failed: %w", err)
	}

	if created {
		return Result{IsOwner: true, Token: entry.Token}, nil
	}

	if entry.Fingerprint != fp {
		return Result{}, fmt.Errorf(
			"idempotency key conflict: stored fingerprint %s differs from incoming %s: %w",
			entry.Fingerprint,
			fp,
			ErrKeyConflict,
		)
	}

	if entry.Response != nil {
		return Result{Response: entry.Response}, nil
	}

	if !e.cfg.WaitForInProgress {
		return Result{}, ErrInProgress
	}

	resp, err := e.waitForResponse(ctx, key, fp)
	if err != nil {
		return Result{}, err
	}

	return Result{Response: resp}, nil
}

// Commit finalizes an operation by persisting the response.
func (e *Engine) Commit(ctx context.Context, key, token string, resp *Response) error {
	if resp == nil {
		return ErrResponseNil
	}

	return e.store.SetResponse(ctx, key, token, cloneResponse(resp), e.cfg.ResultTTL)
}

// Unlock releases an acquired lock without storing a response.
func (e *Engine) Unlock(ctx context.Context, key, token string) error {
	return e.store.Delete(ctx, key, token)
}

func (e *Engine) waitForResponse(
	ctx context.Context,
	storeKey string,
	fp Fingerprint,
) (*Response, error) {
	waitCtx, cancel := context.WithTimeout(ctx, e.cfg.InProgressTimeout)
	defer cancel()

	baseInterval := initialInterval(e.cfg.PollInterval)
	timer := time.NewTimer(e.jitterInterval(baseInterval))
	defer timer.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return nil, waitError(waitCtx)
		case <-timer.C:
			resp, done, err := e.pollOnce(waitCtx, storeKey, fp)
			if err != nil || done {
				return resp, err
			}

			baseInterval = nextInterval(waitCtx, baseInterval)
			timer.Reset(e.jitterInterval(baseInterval))
		}
	}
}

func (e *Engine) pollOnce(ctx context.Context, key string, fp Fingerprint) (*Response, bool, error) {
	entry, err := e.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, false, err
		}

		return nil, false, fmt.Errorf("idempotency store get failed while waiting: %w", err)
	}
	if entry.Fingerprint != fp {
		return nil, false, fmt.Errorf(
			"idempotency key conflict while waiting: stored fingerprint %s differs from incoming %s: %w",
			entry.Fingerprint,
			fp,
			ErrKeyConflict,
		)
	}
	if entry.Response != nil {
		return entry.Response, true, nil
	}

	return nil, false, nil
}

func initialInterval(poll time.Duration) time.Duration {
	if poll <= 0 {
		return defaultPollInterval
	}

	return poll
}

const backoffFactor = 2

func nextInterval(ctx context.Context, cur time.Duration) time.Duration {
	next := cur * backoffFactor
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < next {
		next = time.Until(dl)
	}
	if next <= 0 {
		next = defaultPollInterval
	}

	return next
}

func waitError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrWaitTimeout
	}

	return ctx.Err()
}

const jitterPercent = 20

func (e *Engine) jitterInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}

	maxDelta := d * jitterPercent / 100
	if maxDelta <= 0 {
		return d
	}

	e.rngMu.Lock()
	n := e.rng.Int63n(int64(maxDelta)*2 + 1)
	e.rngMu.Unlock()

	delta := time.Duration(n) - maxDelta
	jittered := d + delta
	if jittered <= 0 {
		return d
	}

	return jittered
}
