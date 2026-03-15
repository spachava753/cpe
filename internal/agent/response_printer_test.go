package agent

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/render"
)

type stubResponseGenerator struct {
	response gai.Response
	err      error
}

func (s *stubResponseGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = dialog
	_ = options
	return s.response, s.err
}

func TestResponsePrinterGenerator_RoutesAllContentBlocksToStdout(t *testing.T) {
	t.Parallel()

	gen := &stubResponseGenerator{response: gai.Response{
		Candidates: []gai.Message{{
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("first")},
				{BlockType: gai.Thinking, ModalityType: gai.Text, Content: gai.Str("thought")},
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("second")},
			},
		}},
	}}

	var stdout, stderr bytes.Buffer
	printer := NewResponsePrinterGenerator(
		gen,
		&render.PlainTextRenderer{},
		&render.PlainTextRenderer{},
		&render.PlainTextRenderer{},
		&stdout,
		&stderr,
	)

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got := stdout.String(); got != "firstsecond" {
		t.Fatalf("stdout = %q, want %q", got, "firstsecond")
	}
	if got := stderr.String(); got != "thought" {
		t.Fatalf("stderr = %q, want %q", got, "thought")
	}
}

func TestResponsePrinterGenerator_PreservesAndPrintsPartialResponseOnError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("stream interrupted")
	wantResp := gai.Response{
		Candidates: []gai.Message{{
			Blocks: []gai.Block{{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("partial")}},
		}},
	}
	gen := &stubResponseGenerator{response: wantResp, err: wantErr}

	var stdout, stderr bytes.Buffer
	printer := NewResponsePrinterGenerator(
		gen,
		&render.PlainTextRenderer{},
		&render.PlainTextRenderer{},
		&render.PlainTextRenderer{},
		&stdout,
		&stderr,
	)

	gotResp, err := printer.Generate(context.Background(), nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if gotResp.Candidates[0].Blocks[0].Content.String() != "partial" {
		t.Fatalf("partial response content = %q, want %q", gotResp.Candidates[0].Blocks[0].Content.String(), "partial")
	}
	if got := stdout.String(); got != "partial" {
		t.Fatalf("stdout = %q, want %q", got, "partial")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty string", got)
	}
}
