package redis

import (
	"time"

	"github.com/velmie/idempo"
)

// Codec encodes and decodes entries for storage in Redis.
type Codec interface {
	MarshalEntry(e *idempo.Entry) ([]byte, error)
	UnmarshalEntry(data []byte) (*idempo.Entry, error)
}

// Options configure Store behavior.
type Options struct {
	KeyPrefix string
	Codec     Codec
	Now       func() time.Time
}

// Option mutates Options.
type Option func(*Options)

// WithKeyPrefix sets a prefix applied to all Redis keys.
func WithKeyPrefix(prefix string) Option {
	return func(o *Options) {
		o.KeyPrefix = prefix
	}
}

// WithCodec sets a custom codec.
func WithCodec(c Codec) Option {
	return func(o *Options) {
		o.Codec = c
	}
}

// WithNow overrides time source.
func WithNow(now func() time.Time) Option {
	return func(o *Options) {
		o.Now = now
	}
}

const defaultKeyPrefix = "idempotency"

func (o *Options) withDefaults() {
	if o.KeyPrefix == "" {
		o.KeyPrefix = defaultKeyPrefix
	}
	if o.Codec == nil {
		o.Codec = JSONCodec{}
	}
	if o.Now == nil {
		o.Now = func() time.Time {
			return time.Now().UTC()
		}
	}
}
