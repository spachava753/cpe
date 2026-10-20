package conversation

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/gai"
)

func TestCompactionCheckpointBoundary(t *testing.T) {
	call, err := gai.ToolCallBlock(
		"effect",
		"bash",
		map[string]any{
			"command": "pwd",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		summary   string
		boundary  int
		wantError bool
	}{
		{
			name:     "complete batch",
			summary:  "Completed work.",
			boundary: 3,
		},
		{
			name:      "negative boundary",
			summary:   "Completed work.",
			boundary:  -1,
			wantError: true,
		},
		{
			name:      "past transcript",
			summary:   "Completed work.",
			boundary:  4,
			wantError: true,
		},
		{
			name:      "splits tool batch",
			summary:   "Completed work.",
			boundary:  2,
			wantError: true,
		},
		{
			name:      "empty summary",
			boundary:  3,
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				stored := storedRecord{
					Compaction: &Compaction{
						Summary:       test.summary,
						AfterMessages: test.boundary,
					},
				}
				for _, message := range []gai.Message{
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock("work"),
						},
					},
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							call,
						},
					},
					gai.ToolResultMessage(
						"effect",
						gai.TextBlock("/work"),
					),
				} {
					item, err := storeMessage(message)
					if err != nil {
						t.Fatal(err)
					}
					stored.Dialog = append(
						stored.Dialog,
						item,
					)
				}
				data, err := json.Marshal(stored)
				if err != nil {
					t.Fatal(err)
				}
				_, err = decodeRecord(data)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"decode checkpoint: %v",
						err,
					)
				}
			},
		)
	}
}
