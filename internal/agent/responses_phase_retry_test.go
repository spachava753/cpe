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

func TestResponsesPhaseRetryGenerator_RetriesPhaseConflictAndSucceeds(t *testing.T) {
	inner := &scriptedPhaseRetryGenerator{
		responses: []gai.Response{{}, successfulAssistantResponse("ok")},
		errors: []error{
			errors.New(`responses output contained multiple assistant message phases ("commentary", "final_answer")`),
			nil,
		},
	}
	gen := newResponsesPhaseRetryGenerator(inner)

	resp, err := gen.Generate(t.Context(), singleUserDialog(), nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("calls = %d, want 2", inner.calls)
	}
	if got := resp.Candidates[0].Blocks[0].Content.String(); got != "ok" {
		t.Fatalf("response content = %q, want ok", got)
	}
}

func TestResponsesPhaseRetryGenerator_ExhaustsPhaseConflictRetries(t *testing.T) {
	phaseErr := errors.New(`responses output contained multiple assistant message phases ("commentary", "final_answer")`)
	inner := &scriptedPhaseRetryGenerator{
		errors: []error{phaseErr, phaseErr, phaseErr, phaseErr, phaseErr, phaseErr},
	}
	gen := newResponsesPhaseRetryGenerator(inner)

	_, err := gen.Generate(t.Context(), singleUserDialog(), nil)
	if !errors.Is(err, phaseErr) {
		t.Fatalf("Generate() error = %v, want final phase error", err)
	}
	if inner.calls != 6 {
		t.Fatalf("calls = %d, want 6", inner.calls)
	}
}

func TestResponsesPhaseRetryGenerator_DoesNotRetryOtherErrors(t *testing.T) {
	wantErr := errors.New("responses prompt cache key must be a string")
	inner := &scriptedPhaseRetryGenerator{errors: []error{wantErr}}
	gen := newResponsesPhaseRetryGenerator(inner)

	_, err := gen.Generate(t.Context(), singleUserDialog(), nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Generate() error = %v, want %v", err, wantErr)
	}
	if inner.calls != 1 {
		t.Fatalf("calls = %d, want 1", inner.calls)
	}
}

func TestResponsesPhaseRetryGenerator_ContextCancellationStopsRetryLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inner := &scriptedPhaseRetryGenerator{
		errors: []error{errors.New(`responses output contained multiple assistant message phases ("commentary", "final_answer")`)},
		onCall: cancel,
	}
	gen := newResponsesPhaseRetryGenerator(inner)

	_, err := gen.Generate(ctx, singleUserDialog(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate() error = %v, want context.Canceled", err)
	}
	if inner.calls != 1 {
		t.Fatalf("calls = %d, want 1", inner.calls)
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
