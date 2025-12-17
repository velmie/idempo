package idempo

import "fmt"

// Fingerprint represents a normalized "fingerprint" of an incoming operation.
// Concrete adapters decide how to populate the fields.
type Fingerprint struct {
	Operation string `json:"operation"` // HTTP method
	Target    string `json:"target"`    // HTTP path+query
	// HeadersHash is an optional hash of additional request attributes (for example selected headers).
	HeadersHash string `json:"headers_hash,omitempty"`
	BodyHash    string `json:"body_hash"` // hash of the request payload
}

func (f Fingerprint) String() string {
	return fmt.Sprintf("op=%q target=%q headers=%q body=%q", f.Operation, f.Target, f.HeadersHash, f.BodyHash)
}
