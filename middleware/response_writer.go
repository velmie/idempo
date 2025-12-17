package middleware

import (
	"bytes"
	"io"
	"net/http"
)

const sniffLen = 512

// bufferingResponseWriter captures response for potential storage.
type bufferingResponseWriter struct {
	dst http.ResponseWriter

	header         http.Header
	headerWritten  bool
	explicitHeader bool
	headerSnap     http.Header

	body     *bytes.Buffer
	maxBytes int64
	overflow bool
	stream   bool

	status int
}

func newBufferingResponseWriter(w http.ResponseWriter, maxBytes int64) *bufferingResponseWriter {
	return &bufferingResponseWriter{
		dst:      w,
		header:   w.Header().Clone(),
		body:     getBuffer(),
		maxBytes: maxBytes,
	}
}

func (b *bufferingResponseWriter) Header() http.Header {
	if b.header == nil {
		b.header = make(http.Header)
	}

	return b.header
}

func (b *bufferingResponseWriter) WriteHeader(statusCode int) {
	if b.headerWritten {
		return
	}

	b.explicitHeader = true
	b.status = statusCode
	b.commitHeader()
}

func (b *bufferingResponseWriter) Write(p []byte) (int, error) {
	if !b.headerWritten {
		b.status = http.StatusOK
		b.commitHeader()
	}

	if b.stream {
		return b.dst.Write(p)
	}

	if !b.overflow && b.maxBytes > 0 && int64(b.body.Len()+len(p)) > b.maxBytes {
		b.overflow = true
		if err := b.startStreaming(p); err != nil {
			return 0, err
		}

		return b.dst.Write(p)
	}

	if _, err := b.body.Write(p); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (b *bufferingResponseWriter) flushTo(dst http.ResponseWriter, truncated bool) {
	if b.stream {
		return
	}

	if b.body != nil && b.body.Len() > 0 {
		b.ensureContentType(b.body.Bytes())
	}

	hdr := b.headerForSend()
	if truncated {
		hdr = hdr.Clone()
		hdr.Set("X-Idempotent-Truncated", "true")
	}

	copyHeaders(dst.Header(), hdr)
	dst.WriteHeader(determineStatus(b.status))

	if b.body != nil && b.body.Len() > 0 {
		_, _ = io.Copy(dst, bytes.NewReader(b.body.Bytes()))
	}
}

func (b *bufferingResponseWriter) ensureContentType(sample []byte) {
	if b.explicitHeader {
		return
	}
	if len(sample) == 0 {
		return
	}
	if len(sample) > sniffLen {
		sample = sample[:sniffLen]
	}

	var hdr http.Header
	if b.headerSnap != nil {
		hdr = b.headerSnap
	} else {
		hdr = b.header
		if hdr == nil {
			hdr = make(http.Header)
			b.header = hdr
		}
	}

	if hdr.Get("Content-Type") != "" {
		return
	}

	hdr.Set("Content-Type", http.DetectContentType(sample))
}

func (b *bufferingResponseWriter) headerForStore() http.Header {
	return b.headerForSend()
}

func (b *bufferingResponseWriter) headerForSend() http.Header {
	if b.headerSnap != nil {
		return b.headerSnap
	}
	if b.header == nil {
		return http.Header{}
	}

	return b.header
}

func (b *bufferingResponseWriter) commitHeader() {
	if b.headerWritten {
		return
	}

	b.headerWritten = true
	b.headerSnap = b.header.Clone()
}

func (b *bufferingResponseWriter) startStreaming(firstChunk []byte) error {
	if b.stream {
		return nil
	}

	if b.body != nil && b.body.Len() > 0 {
		b.ensureContentType(b.body.Bytes())
	} else {
		b.ensureContentType(firstChunk)
	}

	hdr := b.headerForSend().Clone()
	hdr.Set("X-Idempotent-Truncated", "true")
	copyHeaders(b.dst.Header(), hdr)
	b.dst.WriteHeader(determineStatus(b.status))

	if b.body != nil && b.body.Len() > 0 {
		if _, err := io.Copy(b.dst, bytes.NewReader(b.body.Bytes())); err != nil {
			return err
		}
	}

	if b.body != nil {
		putBuffer(b.body)
		b.body = nil
	}

	b.stream = true

	return nil
}

func copyHeaders(dst, src http.Header) {
	for k := range dst {
		delete(dst, k)
	}
	for k, vals := range src {
		cp := make([]string, len(vals))
		copy(cp, vals)
		dst[k] = cp
	}
}

// release returns the buffer to pool.
func (b *bufferingResponseWriter) release() {
	if b.body != nil {
		putBuffer(b.body)
	}
	b.body = nil
}
