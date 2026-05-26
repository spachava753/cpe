package sync

import "sync"

// A Mutex is a mutual exclusion lock.
// This is re-exported for convenience from [sync.Mutex]
type Mutex = sync.Mutex

// A Guard wraps a value with a mutex so callers can safely mutate it through Do.
type Guard[T any] struct {
	t  T
	mu *Mutex
}

// NewGuard returns a Guard protecting t.
func NewGuard[T any](t T) Guard[T] {
	return Guard[T]{
		t:  t,
		mu: new(Mutex),
	}
}

// Do runs f while holding g's mutex.
func (g *Guard[T]) Do(f func(t *T) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return f(&g.t)
}
