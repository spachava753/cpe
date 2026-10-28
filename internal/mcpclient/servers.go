package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is one connected server as a session saw it during setup. The tool
// list and server information are fixed for the session's lifetime.
type Server struct {
	lease        *Lease
	Info         mcp.Implementation
	Instructions string
	Tools        []*mcp.Tool
	Spec         ServerSpec
}

// Call invokes a remote tool. The returned result keeps the server's error flag;
// an error means the outcome of the call is unknown.
func (s *Server) Call(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if params == nil || params.Name == "" {
		return nil, errors.New("MCP tool name is required")
	}
	session, err := s.lease.Session(ctx)
	if err != nil {
		return nil, err
	}
	return session.CallTool(
		ctx,
		params,
	)
}

// Failure records a server that was skipped because it could not be used.
type Failure struct {
	Err  error
	Spec ServerSpec
}

// Servers is one session's view of its connected servers.
type Servers struct {
	connected []*Server
	closeOnce sync.Once
}

// Setup connects every spec concurrently, each bounded by timeout, and lists its
// tools. Servers that fail are skipped and reported; they never fail the setup.
// Connected servers are returned in spec order.
func Setup(ctx context.Context, pool *Pool, specs []ServerSpec, timeout time.Duration) (*Servers, []Failure) {
	results := make(
		[]*Server,
		len(specs),
	)
	errs := make(
		[]error,
		len(specs),
	)
	var wait sync.WaitGroup
	for i, spec := range specs {
		wait.Go(func() {
			results[i], errs[i] = setupServer(
				ctx,
				pool,
				spec,
				timeout,
			)
		})
	}
	wait.Wait()
	servers := &Servers{}
	var failures []Failure
	for i, spec := range specs {
		if errs[i] != nil {
			failures = append(
				failures,
				Failure{
					Spec: spec,
					Err:  errs[i],
				},
			)
			continue
		}
		servers.connected = append(
			servers.connected,
			results[i],
		)
	}
	return servers, failures
}

func setupServer(ctx context.Context, pool *Pool, spec ServerSpec, timeout time.Duration) (*Server, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx,
			timeout,
		)
		defer cancel()
	}
	lease, err := pool.Acquire(
		ctx,
		spec.Transport,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"connect: %w",
			err,
		)
	}
	server, err := describe(
		ctx,
		lease,
		spec,
	)
	if err != nil {
		lease.Release()
		return nil, err
	}
	return server, nil
}

func describe(ctx context.Context, lease *Lease, spec ServerSpec) (*Server, error) {
	session, err := lease.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"connect: %w",
			err,
		)
	}
	server := &Server{
		Spec:  spec,
		lease: lease,
	}
	if result := session.InitializeResult(); result != nil {
		server.Instructions = result.Instructions
		if result.ServerInfo != nil {
			server.Info = *result.ServerInfo
		}
	}
	seen := make(map[string]bool)
	for tool, err := range session.Tools(
		ctx,
		nil,
	) {
		if err != nil {
			return nil, fmt.Errorf(
				"list tools: %w",
				err,
			)
		}
		if tool == nil || tool.Name == "" {
			return nil, errors.New("list tools: server returned a tool without a name")
		}
		if seen[tool.Name] {
			return nil, fmt.Errorf(
				"list tools: server returned tool %q more than once",
				tool.Name,
			)
		}
		seen[tool.Name] = true
		server.Tools = append(
			server.Tools,
			tool,
		)
	}
	return server, nil
}

// Connected returns the servers that connected, in spec order.
func (s *Servers) Connected() []*Server {
	return s.connected
}

// Close releases this session's leases; shared connections may stay open for other sessions.
func (s *Servers) Close() {
	s.closeOnce.Do(func() {
		for _, server := range s.connected {
			server.lease.Release()
		}
	})
}
