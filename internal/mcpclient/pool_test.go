package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

// fixtureServer builds an in-memory server with the named tools. Each tool
// echoes its name, so tests can tell which server answered.
func fixtureServer(name string, tools ...string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:        name,
			Version:     "1",
			Description: name + " description",
		},
		&mcp.ServerOptions{
			Instructions: name + " instructions\nsecond line",
		},
	)
	for _, tool := range tools {
		server.AddTool(
			&mcp.Tool{
				Name:        tool,
				Description: tool + " description",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: name + ":" + tool,
						},
					},
				}, nil
			},
		)
	}
	return server
}

// fixtureConnector connects to in-memory servers selected by the transport command.
type fixtureConnector struct {
	t        *testing.T
	servers  map[string]*mcp.Server
	fail     map[string]error
	block    map[string]chan struct{}
	remotes  []*mcp.ServerSession
	connects atomic.Int32
	mu       sync.Mutex
}

func (f *fixtureConnector) connect(ctx context.Context, server plugins.MCPServer, _ io.Writer) (*mcp.ClientSession, error) {
	f.connects.Add(1)
	if err := f.fail[server.Command]; err != nil {
		return nil, err
	}
	if block := f.block[server.Command]; block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	remote := f.servers[server.Command]
	if remote == nil {
		return nil, errors.New("no fixture server")
	}
	a, b := mcp.NewInMemoryTransports()
	session, err := remote.Connect(
		ctx,
		a,
		nil,
	)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.remotes = append(
		f.remotes,
		session,
	)
	f.mu.Unlock()
	f.t.Cleanup(func() { _ = session.Close() })
	return mcp.NewClient(
		&mcp.Implementation{
			Name:    "test",
			Version: "1",
		},
		nil,
	).Connect(
		ctx,
		b,
		nil,
	)
}

func (f *fixtureConnector) closeRemotes() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, remote := range f.remotes {
		_ = remote.Close()
	}
}

func stdioSpec(id, command string) ServerSpec {
	return ServerSpec{
		ID:     ServerID(id),
		Source: SourceSettings,
		Transport: plugins.MCPServer{
			Type:    plugins.Stdio,
			Command: command,
		},
	}
}

func newFixturePool(t *testing.T, connector *fixtureConnector, grace time.Duration) *Pool {
	t.Helper()
	pool, err := NewPool(
		connector.connect,
		grace,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func TestPoolSharesIdenticalConfigurations(t *testing.T) {
	connector := &fixtureConnector{
		t: t,
		servers: map[string]*mcp.Server{
			"a": fixtureServer(
				"a",
				"echo",
			),
			"b": fixtureServer(
				"b",
				"echo",
			),
		},
	}
	pool := newFixturePool(
		t,
		connector,
		0,
	)
	var wait sync.WaitGroup
	leases := make(
		[]*Lease,
		8,
	)
	for i := range leases {
		wait.Go(func() {
			lease, err := pool.Acquire(
				t.Context(),
				stdioSpec(
					"a",
					"a",
				).Transport,
			)
			if err != nil {
				t.Error(err)
				return
			}
			leases[i] = lease
		})
	}
	wait.Wait()
	if got := connector.connects.Load(); got != 1 {
		t.Fatalf(
			"identical configurations opened %d connections, want 1",
			got,
		)
	}
	other, err := pool.Acquire(
		t.Context(),
		stdioSpec(
			"b",
			"b",
		).Transport,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := connector.connects.Load(); got != 2 {
		t.Fatalf(
			"a different configuration opened %d connections in total, want 2",
			got,
		)
	}
	first, err := leases[0].Session(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := leases[7].Session(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("leases on one configuration returned different connections")
	}
	other.Release()
	for _, lease := range leases {
		lease.Release()
		// Releasing twice must not drop another session's reference.
		lease.Release()
	}
	closed := make(chan struct{})
	go func() {
		_ = first.Wait()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("connection stayed open after its last lease was released with no grace period")
	}
}

func TestPoolKeepsIdleConnectionsForGracePeriod(t *testing.T) {
	connector := &fixtureConnector{
		t: t,
		servers: map[string]*mcp.Server{
			"a": fixtureServer(
				"a",
				"echo",
			),
		},
	}
	pool := newFixturePool(
		t,
		connector,
		time.Hour,
	)
	transport := stdioSpec(
		"a",
		"a",
	).Transport
	lease, err := pool.Acquire(
		t.Context(),
		transport,
	)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	again, err := pool.Acquire(
		t.Context(),
		transport,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Release()
	if got := connector.connects.Load(); got != 1 {
		t.Fatalf(
			"reacquiring within the grace period opened %d connections, want 1",
			got,
		)
	}
}

func TestPoolReconnectsBrokenConnections(t *testing.T) {
	connector := &fixtureConnector{
		t: t,
		servers: map[string]*mcp.Server{
			"a": fixtureServer(
				"a",
				"echo",
			),
		},
	}
	pool := newFixturePool(
		t,
		connector,
		0,
	)
	servers, failures := Setup(
		t.Context(),
		pool,
		[]ServerSpec{
			stdioSpec(
				"a",
				"a",
			),
		},
		time.Second,
	)
	defer servers.Close()
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	server := servers.Connected()[0]
	connector.closeRemotes()
	deadline := time.Now().Add(5 * time.Second)
	for {
		result, err := server.Call(
			t.Context(),
			&mcp.CallToolParams{
				Name: "echo",
			},
		)
		if err == nil {
			if text := result.Content[0].(*mcp.TextContent).Text; text != "a:echo" {
				t.Fatalf(
					"reconnected call returned %q",
					text,
				)
			}
			break
		}
		// The in-flight call after a break fails; a later call reconnects.
		if time.Now().After(deadline) {
			t.Fatalf(
				"call never succeeded after reconnecting: %v",
				err,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := connector.connects.Load(); got != 2 {
		t.Fatalf(
			"connections = %d, want a reconnect",
			got,
		)
	}
}

func TestSetupSkipsFailingServers(t *testing.T) {
	connector := &fixtureConnector{
		t: t,
		servers: map[string]*mcp.Server{
			"good": fixtureServer(
				"good",
				"echo",
			),
		},
		fail: map[string]error{
			"bad": errors.New("command not found"),
		},
		block: map[string]chan struct{}{
			"slow": make(chan struct{}),
		},
	}
	pool := newFixturePool(
		t,
		connector,
		0,
	)
	start := time.Now()
	servers, failures := Setup(
		t.Context(),
		pool,
		[]ServerSpec{
			stdioSpec(
				"bad",
				"bad",
			),
			stdioSpec(
				"good",
				"good",
			),
			stdioSpec(
				"slow",
				"slow",
			),
		},
		200*time.Millisecond,
	)
	defer servers.Close()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf(
			"a hung server stalled setup for %s",
			elapsed,
		)
	}
	if len(servers.Connected()) != 1 || servers.Connected()[0].Spec.ID != "good" {
		t.Fatalf(
			"connected = %+v, want only the healthy server",
			servers.Connected(),
		)
	}
	if len(failures) != 2 || failures[0].Spec.ID != "bad" || failures[1].Spec.ID != "slow" {
		t.Fatalf(
			"failures = %+v",
			failures,
		)
	}
	if !strings.Contains(
		failures[0].Err.Error(),
		"command not found",
	) || !errors.Is(
		failures[1].Err,
		context.DeadlineExceeded,
	) {
		t.Fatalf(
			"failure causes = %v, %v",
			failures[0].Err,
			failures[1].Err,
		)
	}
	good := servers.Connected()[0]
	if good.Info.Description != "good description" || !strings.HasPrefix(
		good.Instructions,
		"good instructions",
	) || len(good.Tools) != 1 {
		t.Fatalf(
			"server details = %+v",
			good,
		)
	}
}

func TestPoolRejectsUseAfterClose(t *testing.T) {
	connector := &fixtureConnector{
		t: t,
		servers: map[string]*mcp.Server{
			"a": fixtureServer("a"),
		},
	}
	pool := newFixturePool(
		t,
		connector,
		time.Hour,
	)
	lease, err := pool.Acquire(
		t.Context(),
		stdioSpec(
			"a",
			"a",
		).Transport,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Session(t.Context()); err == nil {
		t.Fatal("closed pool returned a connection")
	}
	lease.Release()
	if _, err := pool.Acquire(
		t.Context(),
		stdioSpec(
			"a",
			"a",
		).Transport,
	); err == nil {
		t.Fatal("closed pool accepted a new lease")
	}
}

func TestPoolWaitersHonorTheirContext(t *testing.T) {
	release := make(chan struct{})
	connector := &fixtureConnector{
		t: t,
		servers: map[string]*mcp.Server{
			"a": fixtureServer("a"),
		},
		block: map[string]chan struct{}{
			"a": release,
		},
	}
	pool := newFixturePool(
		t,
		connector,
		0,
	)
	transport := stdioSpec(
		"a",
		"a",
	).Transport
	first := make(chan error)
	go func() {
		lease, err := pool.Acquire(
			context.Background(),
			transport,
		)
		if err == nil {
			lease.Release()
		}
		first <- err
	}()
	for connector.connects.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	// A second session waiting on the hung connection gives up at its own deadline.
	ctx, cancel := context.WithTimeout(
		t.Context(),
		100*time.Millisecond,
	)
	defer cancel()
	start := time.Now()
	_, err := pool.Acquire(
		ctx,
		transport,
	)
	if !errors.Is(
		err,
		context.DeadlineExceeded,
	) || time.Since(start) > 5*time.Second {
		t.Fatalf(
			"waiter error = %v after %s",
			err,
			time.Since(start),
		)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestConnectionKeyIgnoresIrrelevantFields(t *testing.T) {
	endpoint, err := url.Parse("https://example.com/mcp")
	if err != nil {
		t.Fatal(err)
	}
	httpKey := func(server plugins.MCPServer) string {
		t.Helper()
		key, err := connectionKey(server)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	a := httpKey(plugins.MCPServer{
		Type:    plugins.StreamableHTTP,
		URL:     *endpoint,
		Cwd:     "/project/a",
		Headers: http.Header{"Authorization": {"x"}},
	})
	b := httpKey(plugins.MCPServer{
		Type:    plugins.StreamableHTTP,
		URL:     *endpoint,
		Cwd:     "/project/b",
		Env:     map[string]string{},
		Headers: http.Header{"Authorization": {"x"}},
	})
	if a != b {
		t.Fatalf(
			"equivalent HTTP servers have different keys:\n%s\n%s",
			a,
			b,
		)
	}
	if a == httpKey(plugins.MCPServer{
		Type:    plugins.StreamableHTTP,
		URL:     *endpoint,
		Headers: http.Header{"Authorization": {"y"}},
	}) {
		t.Fatal("different credentials share a key")
	}
	if httpKey(plugins.MCPServer{
		Type:    plugins.Stdio,
		Command: "x",
	}) != httpKey(plugins.MCPServer{
		Type:    plugins.Stdio,
		Command: "x",
		Args:    []string{},
		Env:     map[string]string{},
		Headers: http.Header{},
	}) {
		t.Fatal("empty and missing collections produce different stdio keys")
	}
}

func TestNewSessionsSeeToolsAddedOnSharedConnections(t *testing.T) {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "cached",
			Version: "1",
		},
		&mcp.ServerOptions{
			// The SDK client caches tool lists for this long unless notified.
			SetCacheable: func(_ context.Context, _ mcp.Request, cacheable *mcp.Cacheable) {
				cacheable.TTLMs = 60_000
			},
		},
	)
	addTool := func(name string) {
		server.AddTool(
			&mcp.Tool{
				Name:        name,
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			},
		)
	}
	addTool("first")
	pool, err := NewPool(
		func(ctx context.Context, _ plugins.MCPServer, _ io.Writer) (*mcp.ClientSession, error) {
			a, b := mcp.NewInMemoryTransports()
			remote, err := server.Connect(
				ctx,
				a,
				nil,
			)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = remote.Close() })
			return newClient().Connect(
				ctx,
				b,
				nil,
			)
		},
		time.Hour,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	specs := []ServerSpec{
		stdioSpec(
			"cached",
			"cached",
		),
	}
	first, failures := Setup(
		t.Context(),
		pool,
		specs,
		time.Second,
	)
	defer first.Close()
	if len(failures) != 0 || len(first.Connected()[0].Tools) != 1 {
		t.Fatalf(
			"first session: %+v %+v",
			first.Connected(),
			failures,
		)
	}
	addTool("second")
	deadline := time.Now().Add(5 * time.Second)
	for {
		next, failures := Setup(
			t.Context(),
			pool,
			specs,
			time.Second,
		)
		tools := 0
		if len(failures) == 0 {
			tools = len(next.Connected()[0].Tools)
		}
		next.Close()
		if tools == 2 {
			break
		}
		// The change notification arrives asynchronously.
		if time.Now().After(deadline) {
			t.Fatal("a new session kept the shared connection's cached tool list")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(first.Connected()[0].Tools); got != 1 {
		t.Fatalf(
			"an open session's catalog changed to %d tools",
			got,
		)
	}
}
