package xio

import (
	"sync"
	"unicode/utf8"
)

// TailBuffer is an io.Writer that retains the end of the bytes written to it.
// A non-positive limit disables truncation. When truncation happens, the
// retained bytes are advanced to a UTF-8 rune boundary so String returns valid
// text when the input was valid UTF-8.
type TailBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

// NewTailBuffer returns a TailBuffer that retains at most limit bytes from the
// end of the stream. A non-positive limit retains all bytes.
func NewTailBuffer(limit int) *TailBuffer {
	return &TailBuffer{limit: limit}
}

// Write appends p to the buffer, retaining only the configured tail. It always
// reports that all bytes were accepted.
func (b *TailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.limit <= 0 {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)
		b.truncated = true
		b.trimLeadingPartialRuneLocked()
		return len(p), nil
	}
	if overflow := len(b.buf) + len(p) - b.limit; overflow > 0 {
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
		b.truncated = true
		b.trimLeadingPartialRuneLocked()
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *TailBuffer) trimLeadingPartialRuneLocked() {
	for len(b.buf) > 0 && !utf8.RuneStart(b.buf[0]) {
		b.buf = b.buf[1:]
	}
}

// String returns the currently retained bytes as a string.
func (b *TailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// Truncated reports whether bytes have been discarded from the beginning of the
// stream because the configured limit was exceeded.
func (b *TailBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
