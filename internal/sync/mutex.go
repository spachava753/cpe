package sync

import "sync"

// A mutex is a mutual exclusion lock.
// This is re-exported for convenience from [sync.mutex]
type mutex = sync.Mutex

// A Guard wraps a value with a mutex so callers can safely mutate it through Do.
type Guard[T any] struct {
	mu mutex
	t  T
}

// NewGuard returns a Guard protecting t.
func NewGuard[T any](t T) *Guard[T] {
	return &Guard[T]{t: t}
}

// Do runs f while holding g's mutex.
func (g *Guard[T]) Do(f func(t *T) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return f(&g.t)
}
