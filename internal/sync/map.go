package sync

// The code in this file was taken from https://github.com/tailscale/tailscale/blob/8f2c8d6a14419e95fb9d02d6bf6113893daef5c3/syncs/syncs.go#L241-L411

import (
	"iter"
	"sync"
)

// Map is a Go map protected by a [sync.RWMutex].
// It is preferred over [sync.Map] for maps with entries that change
// at a relatively high frequency.
// This must not be shallow copied.
type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// Load loads the value for the provided key and whether it was found.
func (m *Map[K, V]) Load(key K) (value V, loaded bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, loaded = m.m[key]
	return value, loaded
}

// LoadFunc calls f with the value for the provided key
// regardless of whether the entry exists or not.
// The lock is held for the duration of the call to f.
func (m *Map[K, V]) LoadFunc(key K, f func(value V, loaded bool)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, loaded := m.m[key]
	f(value, loaded)
}

// Store stores the value for the provided key.
func (m *Map[K, V]) Store(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	Set(&m.m, key, value)
}

// LoadOrStore returns the value for the given key if it exists
// otherwise it stores value.
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	if actual, loaded = m.Load(key); loaded {
		return actual, loaded
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	actual, loaded = m.m[key]
	if !loaded {
		actual = value
		Set(&m.m, key, value)
	}
	return actual, loaded
}

// LoadOrInit returns the value for the given key if it exists
// otherwise f is called to construct the value to be set.
// The lock is held for the duration to prevent duplicate initialization.
func (m *Map[K, V]) LoadOrInit(key K, f func() V) (actual V, loaded bool) {
	if actual, loaded := m.Load(key); loaded {
		return actual, loaded
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if actual, loaded = m.m[key]; loaded {
		return actual, loaded
	}

	loaded = false
	actual = f()
	Set(&m.m, key, actual)
	return actual, loaded
}

// LoadAndDelete returns the value for the given key if it exists.
// It ensures that the map is cleared of any entry for the key.
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, loaded = m.m[key]
	if loaded {
		delete(m.m, key)
	}
	return value, loaded
}

// Delete deletes the entry identified by key.
func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, key)
}

// Keys iterates over all keys in the map in an undefined order.
// A read lock is held for the entire duration of the iteration.
// Use the [WithLock] method instead to mutate the map during iteration.
func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		for k := range m.m {
			if !yield(k) {
				return
			}
		}
	}
}

// Values iterates over all values in the map in an undefined order.
// A read lock is held for the entire duration of the iteration.
// Use the [WithLock] method instead to mutate the map during iteration.
func (m *Map[K, V]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		for _, v := range m.m {
			if !yield(v) {
				return
			}
		}
	}
}

// All iterates over all entries in the map in an undefined order.
// A read lock is held for the entire duration of the iteration.
// Use the [WithLock] method instead to mutate the map during iteration.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		for k, v := range m.m {
			if !yield(k, v) {
				return
			}
		}
	}
}

// WithLock calls f with the underlying map.
// Use of m2 must not escape the duration of this call.
// The write-lock is held for the entire duration of this call.
func (m *Map[K, V]) WithLock(f func(m2 map[K]V)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	f(m.m)
}

// Len returns the length of the map.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.m)
}

// Clear removes all entries from the map.
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.m)
}

// Swap stores the value for the provided key, and returns the previous value
// (if any). If there was no previous value set, a zero value will be returned.
func (m *Map[K, V]) Swap(key K, value V) (oldValue V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldValue = m.m[key]
	Set(&m.m, key, value)
	return oldValue
}

// Set populates an entry in a map, making the map if necessary.
//
// That is, it assigns (*m)[k] = v, making *m if it was nil.
func Set[K comparable, V any, T ~map[K]V](m *T, k K, v V) {
	if *m == nil {
		*m = make(map[K]V)
	}
	(*m)[k] = v
}
