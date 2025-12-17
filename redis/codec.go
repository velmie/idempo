package redis

import (
	"encoding/json"
	"fmt"

	"github.com/velmie/idempo"
)

// JSONCodec implements Codec using encoding/json.
type JSONCodec struct{}

func (JSONCodec) MarshalEntry(e *idempo.Entry) ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal entry: %w", err)
	}

	return data, nil
}

func (JSONCodec) UnmarshalEntry(data []byte) (*idempo.Entry, error) {
	var entry idempo.Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal entry: %w", err)
	}

	return &entry, nil
}
