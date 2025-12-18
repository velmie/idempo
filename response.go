package idempo

// Response is a transport-agnostic representation of a handler result.
// Engine clones Body/Metadata before persisting to avoid races with caller mutations.
type Response struct {
	StatusCode int                 `json:"status_code"`
	Metadata   map[string][]string `json:"metadata,omitempty"`
	Body       []byte              `json:"body,omitempty"`
	// Truncated marks that the original body was larger than adapter limit and was not stored.
	Truncated bool `json:"truncated,omitempty"`
}

// CloneResponse deep-copies the response, including metadata and body.
func CloneResponse(src *Response) *Response {
	return cloneResponse(src)
}

func cloneResponse(src *Response) *Response {
	if src == nil {
		return nil
	}

	cp := *src
	if src.Body != nil {
		cp.Body = append([]byte(nil), src.Body...)
	}
	if src.Metadata != nil {
		cp.Metadata = make(map[string][]string, len(src.Metadata))
		for k, vals := range src.Metadata {
			cp.Metadata[k] = append([]string(nil), vals...)
		}
	}

	return &cp
}
