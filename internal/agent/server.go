package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// Server serves one ACP connection and owns its live sessions. Use Serve.
type Server struct {
	ac       *acp.AgentConnection
	config   Config
	deps     Dependencies
	lifetime context.Context
	cancel   context.CancelFunc
	// lifecycle serializes session creation, restore, close, fork, and deletion.
	// Prompts and cancellation never acquire it.
	lifecycle   sync.Mutex
	mu          sync.Mutex
	initialized bool
	notices     bool
	closed      bool
	sessions    map[SessionID]*session
}

// Serve runs one ACP connection and joins all session work before returning.
// Dependencies are borrowed; their owner closes them after Serve returns.
func Serve(ctx context.Context, transport acp.Transport, config Config, deps Dependencies) error {
	if transport == nil {
		return errors.New("ACP transport is required")
	}
	s, err := newServer(
		ctx,
		config,
		deps,
	)
	if err != nil {
		return err
	}
	//nolint:contextcheck // lifetime is derived from ctx and cancelled on transport failure.
	err = acp.RunAgent(
		s.lifetime,
		lifetimeTransport{
			base:     transport,
			lifetime: s.lifetime,
			cancel:   s.cancel,
		},
		func(ac *acp.AgentConnection) any {
			s.ac = ac
			return s
		},
	)
	s.cancel()
	cleanup, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		10*time.Second,
	)
	defer cancel()
	return errors.Join(
		err,
		s.close(cleanup),
	)
}

func newServer(ctx context.Context, config Config, deps Dependencies) (*Server, error) {
	if err := checkConfig(
		config,
		deps,
	); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(ctx)
	return &Server{
		config:   config,
		deps:     deps,
		lifetime: lifetime,
		cancel:   cancel,
		sessions: make(map[SessionID]*session),
	}, nil
}

func (s *Server) ready() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.lifetime.Err() != nil {
		return errors.New("ACP connection is closed")
	}
	if !s.initialized {
		return errors.New("initialize must precede session operations")
	}
	return nil
}

func (s *Server) findSession(id SessionID) (*session, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	live, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf(
			"session %q is not open",
			id,
		)
	}
	return live, nil
}

// openSession requires lifecycle ownership and publishes only fully initialized sessions.
func (s *Server) openSession(ctx context.Context, spec sessionSpec, record SessionRecord) (*session, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	_, exists := s.sessions[spec.id]
	s.mu.Unlock()
	if exists {
		return nil, errors.New("session is already open")
	}
	setup, cancel := context.WithTimeout(
		ctx,
		s.config.TurnTimeout,
	)
	defer cancel()
	//nolint:contextcheck // Setup must stop on either request or connection cancellation.
	stop := context.AfterFunc(
		s.lifetime,
		cancel,
	)
	defer stop()
	live, err := newSession(
		setup,
		s.lifetime,
		spec,
		record,
		s.config,
		s.deps,
		&sessionClient{
			ac:      s.ac,
			id:      spec.id,
			notices: s.notices,
		},
	)
	if err != nil {
		return nil, err
	}
	if err := s.deps.History.Save(
		setup,
		live.record,
	); err != nil {
		return nil, errors.Join(
			err,
			live.close(context.WithoutCancel(ctx)),
		)
	}
	s.mu.Lock()
	s.sessions[spec.id] = live
	s.mu.Unlock()
	return live, nil
}

func (s *Server) close(ctx context.Context) error {
	s.cancel()
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	s.closed = true
	live := s.sessions
	s.sessions = make(map[SessionID]*session)
	s.mu.Unlock()
	// Cancel every session first so shutdown does not serialize cancellation.
	for _, session := range live {
		session.cancel()
	}
	var errs []error
	for _, session := range live {
		errs = append(
			errs,
			session.close(ctx),
		)
	}
	return errors.Join(errs...)
}

var (
	_ acp.InitializeHandler   = (*Server)(nil)
	_ acp.AuthenticateHandler = (*Server)(nil)
	_ acp.LogoutHandler       = (*Server)(nil)
	_ acp.SessionHandler      = (*Server)(nil)
)
