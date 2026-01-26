package subagentlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// EventHandler is a callback function for processing received events
type EventHandler func(Event)

// Server receives subagent events via HTTP POST requests
type Server struct {
	handler    EventHandler
	httpServer *http.Server
	listener   net.Listener
}

// NewServer creates a new event server with the given handler
func NewServer(handler EventHandler) *Server {
	return &Server{
		handler: handler,
	}
}

// Start starts the HTTP server on a random available port.
// It returns the full address (e.g., "http://127.0.0.1:12345") once the server is ready.
// The server shuts down when ctx is cancelled.
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
