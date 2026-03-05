package commands

import (
	"context"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func TestSaveSubagentTrace(t *testing.T) {
	db := storage.NewMemDB()
	ctx := context.Background()

	userMsg := gai.Message{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("subagent prompt")},
	}
	assistantMsgs := gai.Dialog{
		{
			Role:   gai.Assistant,
			Blocks: []gai.Block{gai.TextBlock("first response")},
		},
		{
			Role:   gai.Assistant,
			Blocks: []gai.Block{gai.TextBlock("second response")},
		},
	}

	if err := saveSubagentTrace(ctx, db, userMsg, assistantMsgs); err != nil {
		t.Fatalf("saveSubagentTrace returned error: %v", err)
	}

	msgs, err := db.ListMessages(ctx, storage.ListMessagesOptions{AscendingOrder: true})
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}

	count := 0
	for msg := range msgs {
		count++
		isSubagent, ok := msg.ExtraFields[storage.MessageIsSubagentKey].(bool)
		if !ok || !isSubagent {
			t.Fatalf("message %d missing MessageIsSubagentKey=true, got %v", count, msg.ExtraFields[storage.MessageIsSubagentKey])
		}
	}
	if count != 3 {
		t.Fatalf("expected 3 persisted messages, got %d", count)
	}
}
