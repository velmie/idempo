package middleware

import (
	"bytes"
	"sync"
)

var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func getBuffer() *bytes.Buffer {
	buf, ok := bufPool.Get().(*bytes.Buffer)
	if ok {
		return buf
	}

	return new(bytes.Buffer)
}

func putBuffer(buf *bytes.Buffer) {
	buf.Reset()
	bufPool.Put(buf)
}
