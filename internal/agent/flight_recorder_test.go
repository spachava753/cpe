package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

type stubFlightRecorderGenerator struct {
	resp gai.Response
	err  error
}

func (s stubFlightRecorderGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = dialog
	_ = options
	return s.resp, s.err
}

func TestWriteFlightRecorderTraceInactive(t *testing.T) {
	_, err := writeFlightRecorderTrace(nil)
	if err == nil {
		t.Fatal("expected error for inactive recorder")
	}
	if err.Error() != "flight recorder is inactive" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFlightRecorderGenerateAddsHintOnError(t *testing.T) {
	originalTempDir := os.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	if originalTempDir == os.TempDir() {
		t.Fatal("expected TMPDIR override to change temp dir")
	}

	wrapped := gai.Wrap(
		stubFlightRecorderGenerator{err: errors.New("boom")},
		WithFlightRecorder(
			config.Model{Type: "openai", ID: "gpt-test"},
			&config.ResolvedFlightRecorderConfig{Enabled: true, MinAge: config.DefaultFlightRecorderMinAge, MaxBytes: config.DefaultFlightRecorderMaxBytes},
		),
	)
	_, err := wrapped.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var hint interface{ GenerationHint() string }
	if !errors.As(err, &hint) {
		t.Fatal("expected generation hint provider")
	}
	gotHint := hint.GenerationHint()
	const prefix = "Flight trace saved to "
	if len(gotHint) <= len(prefix) || gotHint[:len(prefix)] != prefix {
		t.Fatalf("unexpected hint: %q", gotHint)
	}

	tracePath := gotHint[len(prefix):]
	if filepath.Dir(tracePath) != os.TempDir() {
		t.Fatalf("trace path dir = %q, want %q", filepath.Dir(tracePath), os.TempDir())
	}
	info, statErr := os.Stat(tracePath)
	if statErr != nil {
		t.Fatalf("expected trace file to exist: %v", statErr)
	}
	if info.Size() == 0 {
		t.Fatal("expected non-empty trace file")
	}
	if err.Error() != "boom" {
		t.Fatalf("wrapped error text = %q, want %q", err.Error(), "boom")
	}
}

func TestFlightRecorderGenerateSkipsHintOnSuccess(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	wrapped := gai.Wrap(
		stubFlightRecorderGenerator{resp: gai.Response{FinishReason: gai.EndTurn}},
		WithFlightRecorder(
			config.Model{Type: "openai", ID: "gpt-test"},
			&config.ResolvedFlightRecorderConfig{Enabled: true, MinAge: config.DefaultFlightRecorderMinAge, MaxBytes: config.DefaultFlightRecorderMaxBytes},
		),
	)
	_, err := wrapped.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, readErr := os.ReadDir(os.TempDir())
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no trace files on success, found %d entries", len(entries))
	}
}

func TestFlightRecorderGenerateDisabledPassthrough(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	wrapped := gai.Wrap(
		stubFlightRecorderGenerator{err: errors.New("boom")},
		WithFlightRecorder(
			config.Model{Type: "openai", ID: "gpt-test"},
			&config.ResolvedFlightRecorderConfig{Enabled: false, MinAge: config.DefaultFlightRecorderMinAge, MaxBytes: config.DefaultFlightRecorderMaxBytes},
		),
	)
	_, err := wrapped.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "boom" {
		t.Fatalf("error = %q, want %q", err.Error(), "boom")
	}
	var hint interface{ GenerationHint() string }
	if errors.As(err, &hint) {
		t.Fatal("did not expect generation hint when flight recorder is disabled")
	}
	entries, readErr := os.ReadDir(os.TempDir())
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no trace files when disabled, found %d entries", len(entries))
	}
}
