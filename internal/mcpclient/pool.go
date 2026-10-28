package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

// Pool shares MCP connections between the sessions of one CPE process.
// Connections are keyed by their transport configuration, so sessions with
// identical configuration share one connection while any difference, such as
// per-session credentials in headers, yields a separate one.
type Pool struct {
	connector Connector
	stderr    io.Writer
	entries   map[string]*poolEntry
	// closeErrs collects failures from connections closed in the background.
	closeErrs []error
	closing   sync.WaitGroup
	grace     time.Duration
	mu        sync.Mutex
	closed    bool
}

// NewPool creates an empty pool. After the last lease on a connection is
// released, the connection stays open for grace so that a session opened soon
// after does not restart its servers. stderr receives stdio server diagnostics
// and may be nil to discard them.
func NewPool(connector Connector, grace time.Duration, stderr io.Writer) (*Pool, error) {
	if connector == nil {
		return nil, errors.New("MCP connector is required")
	}
	if grace < 0 {
		return nil, errors.New("MCP connection grace period must not be negative")
	}
	return &Pool{
		connector: connector,
		stderr:    stderr,
		grace:     grace,
		entries:   make(map[string]*poolEntry),
	}, nil
}

// poolEntry owns one shared connection. connect is a one-slot semaphore that
// serializes connection setup, so concurrent first users open one connection
// and a waiter can still give up when its context ends.
type poolEntry struct {
	session *mcp.ClientSession
	idle    *time.Timer
	connect chan struct{}
	key     string
	server  plugins.MCPServer
	refs    int
	broken  bool
}

// Lease is one session's reference to a shared connection.
type Lease struct {
	pool     *Pool
	entry    *poolEntry
	released sync.Once
}

// connectionKey identifies a connection by the fields its transport uses, with
// empty collections normalized, so equivalent configurations from different
// sources share one connection.
func connectionKey(server plugins.MCPServer) (string, error) {
	var key any
	switch server.Type {
	case plugins.Stdio:
		env := make(
			[]string,
			0,
			len(server.Env),
		)
		for _, name := range slices.Sorted(maps.Keys(server.Env)) {
			env = append(
				env,
				name+"="+server.Env[name],
			)
		}
		args := slices.Clone(server.Args)
		if args == nil {
			args = []string{}
		}
		key = []any{
			server.Type,
			server.Command,
			args,
			env,
			server.Cwd,
		}
	case plugins.StreamableHTTP, plugins.SSE:
		headers := make(
			[][]string,
			0,
			len(server.Headers),
		)
		for _, name := range slices.Sorted(maps.Keys(server.Headers)) {
			headers = append(
				headers,
				append(
					[]string{
						http.CanonicalHeaderKey(name),
					},
					server.Headers[name]...,
				),
			)
		}
		key = []any{
			server.Type,
			server.URL.String(),
			headers,
		}
	default:
		return "", errors.New("unsupported MCP transport")
	}
	encoded, err := json.Marshal(key)
	return string(encoded), err
}

// Acquire returns a lease on the connection for server, connecting if needed.
func (p *Pool) Acquire(ctx context.Context, server plugins.MCPServer) (*Lease, error) {
	key, err := connectionKey(server)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("MCP connection pool is closed")
	}
	entry := p.entries[key]
	if entry == nil {
		entry = &poolEntry{
			key:    key,
			server: server,
			connect: make(
				chan struct{},
				1,
			),
		}
		p.entries[key] = entry
	}
	entry.refs++
	if entry.idle != nil {
		entry.idle.Stop()
		entry.idle = nil
	}
	p.mu.Unlock()
	lease := &Lease{
		pool:  p,
		entry: entry,
	}
	if _, err := lease.session(ctx); err != nil {
		lease.Release()
		return nil, err
	}
	return lease, nil
}

// session returns a live connection, reconnecting once the previous one broke.
func (l *Lease) session(ctx context.Context) (*mcp.ClientSession, error) {
	entry := l.entry
	select {
	case entry.connect <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-entry.connect }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.pool.mu.Lock()
	closed := l.pool.closed
	current := entry.session
	broken := entry.broken
	l.pool.mu.Unlock()
	if closed {
		return nil, errors.New("MCP connection pool is closed")
	}
	if current != nil && !broken {
		return current, nil
	}
	if current != nil {
		// The transport already failed; closing releases a stdio process that may linger.
		_ = current.Close()
	}
	session, err := l.pool.connector(
		ctx,
		entry.server,
		l.pool.stderr,
	)
	if err != nil {
		l.pool.mu.Lock()
		entry.session = nil
		entry.broken = false
		l.pool.mu.Unlock()
		return nil, err
	}
	l.pool.mu.Lock()
	if l.pool.closed {
		l.pool.mu.Unlock()
		_ = session.Close()
		return nil, errors.New("MCP connection pool is closed")
	}
	entry.session = session
	entry.broken = false
	l.pool.mu.Unlock()
	go func() {
		_ = session.Wait()
		l.pool.mu.Lock()
		if entry.session == session {
			entry.broken = true
		}
		l.pool.mu.Unlock()
	}()
	return session, nil
}

// Session returns the shared connection, reconnecting if it has broken.
func (l *Lease) Session(ctx context.Context) (*mcp.ClientSession, error) {
	return l.session(ctx)
}

// Release drops this lease. Releasing more than once has no further effect.
func (l *Lease) Release() {
	l.released.Do(func() {
		p := l.pool
		entry := l.entry
		p.mu.Lock()
		defer p.mu.Unlock()
		entry.refs--
		if entry.refs > 0 || p.closed {
			return
		}
		if p.grace == 0 {
			p.closeEntryLocked(entry)
			return
		}
		entry.idle = time.AfterFunc(
			p.grace,
			func() {
				p.mu.Lock()
				defer p.mu.Unlock()
				if entry.refs == 0 && p.entries[entry.key] == entry {
					p.closeEntryLocked(entry)
				}
			},
		)
	})
}

// closeEntryLocked removes an unused entry. Closing happens in the background
// so a slow server shutdown never blocks other pool users; Close waits for it.
func (p *Pool) closeEntryLocked(entry *poolEntry) {
	delete(
		p.entries,
		entry.key,
	)
	if entry.idle != nil {
		entry.idle.Stop()
		entry.idle = nil
	}
	if session := entry.session; session != nil {
		entry.session = nil
		p.closing.Go(func() {
			if err := session.Close(); err != nil {
				p.mu.Lock()
				p.closeErrs = append(
					p.closeErrs,
					err,
				)
				p.mu.Unlock()
			}
		})
	}
}

// Close closes every connection, including leased ones, rejects new leases,
// and waits for connections already closing in the background.
func (p *Pool) Close() error {
	p.mu.Lock()
	p.closed = true
	entries := p.entries
	p.entries = make(map[string]*poolEntry)
	var sessions []*mcp.ClientSession
	for _, entry := range entries {
		if entry.idle != nil {
			entry.idle.Stop()
			entry.idle = nil
		}
		if entry.session != nil {
			sessions = append(
				sessions,
				entry.session,
			)
			entry.session = nil
		}
	}
	p.mu.Unlock()
	var errs []error
	for _, session := range sessions {
		errs = append(
			errs,
			session.Close(),
		)
	}
	p.closing.Wait()
	p.mu.Lock()
	errs = append(
		errs,
		p.closeErrs...,
	)
	p.closeErrs = nil
	p.mu.Unlock()
	return errors.Join(errs...)
}
