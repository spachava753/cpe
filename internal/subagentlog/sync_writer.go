package subagentlog

import (
	"io"
	"sync"
)

// SyncWriter wraps an io.Writer with a mutex to ensure atomic writes.
// This prevents interleaving when multiple goroutines write concurrently.
type SyncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewSyncWriter creates a new SyncWriter that wraps the given writer.
func NewSyncWriter(w io.Writer) *SyncWriter {
	return &SyncWriter{w: w}
}

// Write implements io.Writer with mutex protection.
func (sw *SyncWriter) Write(p []byte) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

// WriteString writes a string atomically.
func (sw *SyncWriter) WriteString(s string) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return io.WriteString(sw.w, s)
}
