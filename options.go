package idempo

import "time"

// EngineOption configures Engine via functional options.
type EngineOption func(*Config)

// WithLockTTL sets Config.LockTTL (how long an in-flight operation keeps its lock).
func WithLockTTL(d time.Duration) EngineOption {
	return func(c *Config) {
		c.LockTTL = d
	}
}

// WithResultTTL sets Config.ResultTTL (how long a completed response is kept for replay).
func WithResultTTL(d time.Duration) EngineOption {
	return func(c *Config) {
		c.ResultTTL = d
	}
}

// WithWaitForInProgress sets Config.WaitForInProgress.
// When enabled, Engine.Process waits for an in-flight operation with the same key to finish and returns its response.
func WithWaitForInProgress(enabled bool) EngineOption {
	return func(c *Config) {
		c.WaitForInProgress = enabled
	}
}

// WithInProgressTimeout sets Config.InProgressTimeout (maximum time to wait for an in-flight operation).
func WithInProgressTimeout(d time.Duration) EngineOption {
	return func(c *Config) {
		c.InProgressTimeout = d
	}
}

// WithPollInterval sets Config.PollInterval (how often to poll the store while waiting for completion).
func WithPollInterval(d time.Duration) EngineOption {
	return func(c *Config) {
		c.PollInterval = d
	}
}
