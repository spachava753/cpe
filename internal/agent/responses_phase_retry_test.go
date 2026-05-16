package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/spachava753/gai"
)

type scriptedPhaseRetryGenerator struct {
	responses []gai.Response
	errors    []error
	onCall    func()
	calls     int
	tools     []gai.Tool
}

func (g *scriptedPhaseRetryGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = dialog
	_ = opts
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

func (g *scriptedPhaseRetryGenerator) Register(tool gai.Tool) error {
	g.tools = append(g.tools, tool)
	return nil
}

func TestResponsesPhaseRetryGenerator_Generate(t *testing.T) {
	phaseErr := errors.New(`responses output contained multiple assistant message phases ("commentary", "final_answer")`)
	otherErr := errors.New("responses prompt cache key must be a string")

	tests := []struct {
		name      string
		responses []gai.Response
		errors    []error
		setup     func(*testing.T, *scriptedPhaseRetryGenerator) context.Context
		wantErr   error
		wantCalls int
		wantText  string
	}{
		{
			name:      "retries phase conflict and succeeds",
			responses: []gai.Response{{}, successfulAssistantResponse("ok")},
			errors:    []error{phaseErr, nil},
			wantCalls: 2,
			wantText:  "ok",
		},
		{
			name:      "exhausts phase conflict retries",
			errors:    []error{phaseErr, phaseErr, phaseErr, phaseErr, phaseErr, phaseErr},
			wantErr:   phaseErr,
			wantCalls: 6,
		},
		{
			name:      "does not retry other errors",
			errors:    []error{otherErr},
			wantErr:   otherErr,
			wantCalls: 1,
		},
		{
			name:   "context cancellation stops retry loop",
			errors: []error{phaseErr},
			setup: func(t *testing.T, inner *scriptedPhaseRetryGenerator) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				inner.onCall = cancel
				return ctx
			},
			wantErr:   context.Canceled,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := &scriptedPhaseRetryGenerator{
				responses: tt.responses,
				errors:    tt.errors,
			}
			ctx := t.Context()
			if tt.setup != nil {
				ctx = tt.setup(t, inner)
			}
			gen := newResponsesPhaseRetryGenerator(inner)

			resp, err := gen.Generate(ctx, singleUserDialog(), nil)
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

func TestResponsesPhaseRetryGenerator_RegisterDelegates(t *testing.T) {
	inner := &scriptedPhaseRetryGenerator{}
	gen := newResponsesPhaseRetryGenerator(inner)
	tool := gai.Tool{Name: "lookup", Description: "look up a value"}

	if err := gen.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(inner.tools) != 1 || inner.tools[0].Name != tool.Name {
		t.Fatalf("registered tools = %#v, want %q", inner.tools, tool.Name)
	}
}

func singleUserDialog() gai.Dialog {
	return gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}
}

func successfulAssistantResponse(text string) gai.Response {
	return gai.Response{
		Candidates: []gai.Message{{
			Role:   gai.Assistant,
			Blocks: []gai.Block{gai.TextBlock(text)},
		}},
		FinishReason:  gai.EndTurn,
		UsageMetadata: make(gai.Metadata),
	}
}
