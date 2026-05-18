package commands

import (
	"strings"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func TestMessageMetadataInline(t *testing.T) {
	msg := gai.Message{ExtraFields: map[string]any{
		storage.AgentMetadataModelTypeKey:        "zai",
		storage.AgentMetadataModelIDKey:          "glm-5.1",
		storage.AgentMetadataInputTokensKey:      int64(5419),
		storage.AgentMetadataOutputTokensKey:     258,
		storage.AgentMetadataCacheReadTokensKey:  float64(5632),
		storage.AgentMetadataCacheWriteTokensKey: uint(2),
	}}

	want := " [model: zai/glm-5.1] [tokens: in 5419 / out 258 / cache 5632 / cache write 2]"
	if got := messageMetadataInline(msg); got != want {
		t.Fatalf("messageMetadataInline() = %q, want %q", got, want)
	}
}

func TestMessageMetadataInline_OmitsMissingFields(t *testing.T) {
	msg := gai.Message{ExtraFields: map[string]any{
		storage.AgentMetadataModelIDKey:     "glm-5.1",
		storage.AgentMetadataInputTokensKey: int64(1000),
	}}

	if got := messageMetadataInline(msg); got != " [model: glm-5.1] [tokens: in 1000]" {
		t.Fatalf("messageMetadataInline() = %q", got)
	}
}

type identityRenderer struct{}

func (identityRenderer) Render(in string) (string, error) {
	return in, nil
}

func TestMarkdownDialogFormatterIncludesMessageMetadata(t *testing.T) {
	formatter := &MarkdownDialogFormatter{Renderer: identityRenderer{}}
	dialog := gai.Dialog{{
		Role:   gai.Assistant,
		Blocks: []gai.Block{gai.TextBlock("answer")},
		ExtraFields: map[string]any{
			storage.AgentMetadataModelTypeKey:       "zai",
			storage.AgentMetadataModelIDKey:         "glm-5.1",
			storage.AgentMetadataInputTokensKey:     int64(5419),
			storage.AgentMetadataOutputTokensKey:    int64(258),
			storage.AgentMetadataCacheReadTokensKey: int64(5632),
		},
	}}

	got, err := formatter.FormatDialog(dialog, []string{"msg1"})
	if err != nil {
		t.Fatalf("FormatDialog returned error: %v", err)
	}
	for _, want := range []string{
		"## 🤖 ASSISTANT [`msg1`]",
		"> model: `zai/glm-5.1`",
		"> tokens: in `5419`, out `258`, cache `5632`",
		"answer",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted dialog missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "answer") > strings.Index(got, "> model:") {
		t.Fatalf("metadata should be printed after message content:\n%s", got)
	}
}

func TestMarkdownDialogFormatterOmitsMissingMetadata(t *testing.T) {
	formatter := &MarkdownDialogFormatter{Renderer: identityRenderer{}}
	dialog := gai.Dialog{{
		Role:   gai.Assistant,
		Blocks: []gai.Block{gai.TextBlock("answer")},
	}}

	got, err := formatter.FormatDialog(dialog, []string{"msg1"})
	if err != nil {
		t.Fatalf("FormatDialog returned error: %v", err)
	}
	if strings.Contains(got, "> model:") || strings.Contains(got, "> tokens:") {
		t.Fatalf("formatted dialog unexpectedly included metadata:\n%s", got)
	}
}
