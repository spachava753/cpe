package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/spachava753/gai"
)

func TestProcessUserInput_ReadsNonFileStdinReader(t *testing.T) {
	t.Parallel()

	blocks, err := ProcessUserInput(context.Background(), ProcessUserInputOptions{
		Stdin: strings.NewReader("hello from reader"),
	})
	if err != nil {
		t.Fatalf("ProcessUserInput() error = %v", err)
	}

	want := []gai.Block{{
		BlockType:    gai.Content,
		ModalityType: gai.Text,
		MimeType:     "text/plain",
		Content:      gai.Str("hello from reader"),
	}}
	if len(blocks) != len(want) {
		t.Fatalf("len(blocks) = %d, want %d", len(blocks), len(want))
	}
	if blocks[0].BlockType != want[0].BlockType || blocks[0].ModalityType != want[0].ModalityType || blocks[0].MimeType != want[0].MimeType || blocks[0].Content.String() != want[0].Content.String() {
		t.Fatalf("unexpected block = %#v, want %#v", blocks[0], want[0])
	}
}
