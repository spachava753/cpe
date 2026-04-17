package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/render"
	"github.com/spachava753/cpe/internal/storage"
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

type recordingDialogSaver struct {
	nextID int
}

func (s *recordingDialogSaver) SaveDialog(ctx context.Context, msgs iter.Seq[gai.Message]) iter.Seq2[gai.Message, error] {
	return func(yield func(gai.Message, error) bool) {
		_ = ctx
		previousID := ""
		for msg := range msgs {
			saved := msg
			if saved.ExtraFields == nil {
				saved.ExtraFields = make(map[string]any)
			}
			if GetMessageID(saved) == "" {
				s.nextID++
				saved.ExtraFields[storage.MessageIDKey] = fmt.Sprintf("msg_%d", s.nextID)
			}
			if previousID != "" {
				saved.ExtraFields[storage.MessageParentIDKey] = previousID
			}
			previousID = GetMessageID(saved)
			if !yield(saved, nil) {
				return
			}
		}
	}
}

func newPlainTurnLifecycleMiddleware(
	gen gai.Generator,
	stdout io.Writer,
	stderr io.Writer,
	model config.Model,
	saver storage.DialogSaver,
	disableResponsePrinting bool,
) *TurnLifecycleMiddleware {
	middleware := NewTurnLifecycleMiddleware(gen, model, saver, stdout, stderr, disableResponsePrinting)
	middleware.metadataRenderer = &render.PlainTextRenderer{}
	if !disableResponsePrinting {
		middleware.contentRenderer = &render.PlainTextRenderer{}
		middleware.thinkingRenderer = &render.PlainTextRenderer{}
		middleware.toolCallRenderer = &render.PlainTextRenderer{}
	}
	return middleware
}

func TestTurnLifecycleMiddleware_RoutesAllContentBlocksToStdout(t *testing.T) {
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
	middleware := newPlainTurnLifecycleMiddleware(gen, &stdout, &stderr, config.Model{}, nil, false)

	if _, err := middleware.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got := stdout.String(); got != "firstsecond" {
		t.Fatalf("stdout = %q, want %q", got, "firstsecond")
	}
	if got := stderr.String(); got != "thought" {
		t.Fatalf("stderr = %q, want %q", got, "thought")
	}
}

func TestTurnLifecycleMiddleware_PreservesAndPrintsPartialResponseOnError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("stream interrupted")
	wantResp := gai.Response{
		Candidates: []gai.Message{{
			Blocks: []gai.Block{{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("partial")}},
		}},
	}
	gen := &stubResponseGenerator{response: wantResp, err: wantErr}

	var stdout, stderr bytes.Buffer
	middleware := newPlainTurnLifecycleMiddleware(gen, &stdout, &stderr, config.Model{}, nil, false)

	gotResp, err := middleware.Generate(context.Background(), nil, nil)
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

func TestTurnLifecycleMiddleware_SavesAssistantMessageBeforePrintingMessageID(t *testing.T) {
	t.Parallel()

	gen := &stubResponseGenerator{response: gai.Response{
		Candidates: []gai.Message{{
			Blocks: []gai.Block{{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("saved")}},
		}},
	}}
	saver := &recordingDialogSaver{}

	var stdout, stderr bytes.Buffer
	middleware := newPlainTurnLifecycleMiddleware(gen, &stdout, &stderr, config.Model{}, saver, false)
	dialog := gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}

	resp, err := middleware.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := stdout.String(); got != "saved" {
		t.Fatalf("stdout = %q, want %q", got, "saved")
	}
	if messageID := GetMessageID(resp.Candidates[0]); messageID != "msg_2" {
		t.Fatalf("assistant message_id = %q, want %q", messageID, "msg_2")
	}
	if got := stderr.String(); got != "> message_id: `msg_2`" {
		t.Fatalf("stderr = %q, want %q", got, "> message_id: `msg_2`")
	}
}
