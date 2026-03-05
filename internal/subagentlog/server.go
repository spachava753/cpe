package subagentlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// EventHandler processes one decoded event delivered by Server.
//
// The handler is invoked synchronously inside the HTTP request path; if it blocks,
// the corresponding POST blocks and the emitter may time out.
type EventHandler func(Event)

// Server receives subagent logging events over localhost HTTP.
// It accepts POST requests on /subagent-events and forwards decoded events to the
// configured handler.
type Server struct {
	handler    EventHandler
	httpServer *http.Server
	listener   net.Listener
}

// NewServer constructs an event server. A nil handler is allowed and results in
// acknowledged-but-ignored events.
func NewServer(handler EventHandler) *Server {
	return &Server{
		handler: handler,
	}
}

// Start binds to 127.0.0.1 on an ephemeral port and starts serving events.
//
// Contract:
//   - Returns a ready-to-use base address (for example, "http://127.0.0.1:12345").
//   - Registers only /subagent-events for event ingestion.
//   - Shuts down gracefully (up to 5s) when ctx is cancelled.
//
// The caller is expected to inject the returned address into child environments
// (CPE_SUBAGENT_LOGGING_ADDRESS) so subagents can emit events back to the parent.
func (s *Server) Start(ctx context.Context) (string, error) {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc("/subagent-events", s.handleEvents)

	s.httpServer = &http.Server{
		Handler: mux,
	}

	address := fmt.Sprintf("http://%s", listener.Addr().String())

	// Start serving in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Handle graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx) //nolint:contextcheck // Using Background() is correct for graceful shutdown since parent ctx is cancelled
	}()

	return address, nil
}

// handleEvents validates and decodes one incoming event request.
// It returns:
//   - 405 for non-POST methods
//   - 400 for invalid JSON payloads
//   - 200 when the event has been accepted (and handler invoked if configured)
//
// Because emitters treat non-2xx as fatal, status codes directly control whether
// subagents continue or abort.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "Bad Request: invalid JSON", http.StatusBadRequest)
		return
	}

	if s.handler != nil {
		s.handler(event)
	}

	w.WriteHeader(http.StatusOK)
}
