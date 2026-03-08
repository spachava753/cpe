package commands

import (
	"bytes"
	"context"
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

type stubMessagesLister struct {
	msgs []gai.Message
}

func (s stubMessagesLister) ListMessages(ctx context.Context, opts storage.ListMessagesOptions) (iter.Seq[gai.Message], error) {
	_ = ctx
	_ = opts
	return slices.Values(s.msgs), nil
}

func withPlainTreeStyles(t *testing.T) {
	t.Helper()
	oldUser := userRoleStyle
	oldAssistant := assistantRoleStyle
	oldTool := toolRoleStyle
	oldUnknown := unknownRoleStyle
	userRoleStyle = lipgloss.NewStyle()
	assistantRoleStyle = lipgloss.NewStyle()
	toolRoleStyle = lipgloss.NewStyle()
	unknownRoleStyle = lipgloss.NewStyle()
	t.Cleanup(func() {
		userRoleStyle = oldUser
		assistantRoleStyle = oldAssistant
		toolRoleStyle = oldTool
		unknownRoleStyle = oldUnknown
	})
}

func TestConversationList_PrintsLineageColumn(t *testing.T) {
	withPlainTreeStyles(t)

	createdAt := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	msg := gai.Message{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("compacted summary")},
		ExtraFields: map[string]any{
			storage.MessageIDKey:                 "root1",
			storage.MessageCreatedAtKey:          createdAt,
			storage.MessageCompactionParentIDKey: "parent42",
		},
	}

	var out bytes.Buffer
	err := ConversationList(context.Background(), ConversationListOptions{
		Storage:     stubMessagesLister{msgs: []gai.Message{msg}},
		Writer:      &out,
		TreePrinter: &DefaultTreePrinter{},
	})
	if err != nil {
		t.Fatalf("ConversationList returned error: %v", err)
	}

	want := "root1 (2026-03-05 12:00) [USER] [lineage:parent42] compacted summary\n"
	if out.String() != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out.String(), want)
	}
}

func TestConversationList_PrintsEmptyLineageCleanly(t *testing.T) {
	withPlainTreeStyles(t)

	createdAt := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	msg := gai.Message{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("regular conversation")},
		ExtraFields: map[string]any{
			storage.MessageIDKey:        "root2",
			storage.MessageCreatedAtKey: createdAt,
		},
	}

	var out bytes.Buffer
	err := ConversationList(context.Background(), ConversationListOptions{
		Storage:     stubMessagesLister{msgs: []gai.Message{msg}},
		Writer:      &out,
		TreePrinter: &DefaultTreePrinter{},
	})
	if err != nil {
		t.Fatalf("ConversationList returned error: %v", err)
	}

	want := "root2 (2026-03-05 12:00) [USER] [lineage:-] regular conversation\n"
	if out.String() != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out.String(), want)
	}
}
