package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/spachava753/gai"
	"golang.org/x/net/http2"
)

type scriptedNetworkRetryGenerator struct {
	responses []gai.Response
	errors    []error
	onCall    func()
	calls     int
	tools     []gai.Tool
}

func (g *scriptedNetworkRetryGenerator) Generate(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
	g.calls++
	if g.onCall != nil {
		g.onCall()
	}
	idx := g.calls - 1
	var resp gai.Response
	if idx < len(g.responses) {
		resp = g.responses[idx]
	}
	if idx < len(g.errors) {
		return resp, g.errors[idx]
	}
	return resp, nil
}

func (g *scriptedNetworkRetryGenerator) Register(tool gai.Tool) error {
	g.tools = append(g.tools, tool)
	return nil
}

func TestNetworkRetryGeneratorGenerate(t *testing.T) {
	connectionReset := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
	wrappedConnectionReset := fmt.Errorf("provider stream failed: %w", connectionReset)
	truncatedStream := fmt.Errorf("provider stream failed: %w", io.ErrUnexpectedEOF)
	http2Disconnect := fmt.Errorf("provider stream failed: %w", http2.StreamError{
		StreamID: 7,
		Code:     http2.ErrCodeInternal,
		Cause:    errors.New("received from peer"),
	})
	ordinaryErr := errors.New("invalid tool schema")
	unsupportedProtocol := &url.Error{
		Op:  "Post",
		URL: "example.test",
		Err: errors.New("unsupported protocol scheme"),
	}
	success := gai.Response{
		Candidates: []gai.Message{{
			Role:   gai.Assistant,
			Blocks: []gai.Block{gai.TextBlock("ok")},
		}},
	}

	tests := []struct {
		name      string
		responses []gai.Response
		errors    []error
		wantErr   error
		wantCalls int
		wantText  string
	}{
		{
			name:      "retries wrapped TCP error and succeeds",
			responses: []gai.Response{{}, success},
			errors:    []error{wrappedConnectionReset, nil},
			wantCalls: 2,
			wantText:  "ok",
		},
		{
			name:      "retries truncated HTTP stream and succeeds",
			responses: []gai.Response{{}, success},
			errors:    []error{truncatedStream, nil},
			wantCalls: 2,
			wantText:  "ok",
		},
		{
			name:      "retries HTTP2 disconnect and succeeds",
			responses: []gai.Response{{}, success},
			errors:    []error{http2Disconnect, nil},
			wantCalls: 2,
			wantText:  "ok",
		},
		{
			name: "exhausts three network retries",
			errors: []error{
				wrappedConnectionReset,
				wrappedConnectionReset,
				wrappedConnectionReset,
				wrappedConnectionReset,
			},
			wantErr:   connectionReset,
			wantCalls: 4,
		},
		{
			name:      "does not retry ordinary error",
			errors:    []error{ordinaryErr},
			wantErr:   ordinaryErr,
			wantCalls: 1,
		},
		{
			name:      "does not retry context deadline",
			errors:    []error{context.DeadlineExceeded},
			wantErr:   context.DeadlineExceeded,
			wantCalls: 1,
		},
		{
			name:      "does not retry deterministic URL error",
			errors:    []error{unsupportedProtocol},
			wantErr:   unsupportedProtocol,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := &scriptedNetworkRetryGenerator{
				responses: tt.responses,
				errors:    tt.errors,
			}
			gen := newNetworkRetryGenerator(inner)
			gen.retryDelay = 0

			resp, err := gen.Generate(t.Context(), gai.Dialog{}, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Generate() error = %v, want %v", err, tt.wantErr)
			}
			if inner.calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", inner.calls, tt.wantCalls)
			}
			if tt.wantText != "" {
				if got := resp.Candidates[0].Blocks[0].Content.String(); got != tt.wantText {
					t.Fatalf("response content = %q, want %q", got, tt.wantText)
				}
			}
		})
	}
}

func TestNetworkRetryGeneratorCancellationStopsDelay(t *testing.T) {
	connectionReset := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
	ctx, cancel := context.WithCancel(t.Context())
	inner := &scriptedNetworkRetryGenerator{
		errors: []error{connectionReset},
		onCall: cancel,
	}
	gen := newNetworkRetryGenerator(inner)
	gen.retryDelay = time.Hour

	_, err := gen.Generate(ctx, gai.Dialog{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate() error = %v, want context.Canceled", err)
	}
	if inner.calls != 1 {
		t.Fatalf("calls = %d, want 1", inner.calls)
	}
}

func TestNewNetworkRetryGeneratorDefaults(t *testing.T) {
	gen := newNetworkRetryGenerator(&scriptedNetworkRetryGenerator{})

	if gen.maxRetries != defaultNetworkMaxRetries {
		t.Fatalf("maxRetries = %d, want %d", gen.maxRetries, defaultNetworkMaxRetries)
	}
	if gen.retryDelay != defaultNetworkRetryDelay {
		t.Fatalf("retryDelay = %v, want %v", gen.retryDelay, defaultNetworkRetryDelay)
	}
}

func TestNetworkRetryGeneratorRegisterDelegates(t *testing.T) {
	inner := &scriptedNetworkRetryGenerator{}
	gen := newNetworkRetryGenerator(inner)
	tool := gai.Tool{Name: "lookup", Description: "look up a value"}

	if err := gen.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(inner.tools) != 1 || inner.tools[0].Name != tool.Name {
		t.Fatalf("registered tools = %#v, want %q", inner.tools, tool.Name)
	}
}
