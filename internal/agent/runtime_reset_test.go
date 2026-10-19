package agent

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/spachava753/gai"
)

func TestRuntimeResetContext(t *testing.T) {
	for _, test := range []struct {
		name       string
		reset      *runtimeReset
		compaction *Compaction
		want       []string
	}{
		{
			name: "live runtime",
			want: []string{
				"old task",
				"old answer",
				"new work",
			},
		},
		{
			name: "reset between inherited and new messages",
			reset: &runtimeReset{
				afterMessages: 2,
			},
			want: []string{
				"old task",
				"old answer",
				runtimeResetNotice,
				"new work",
			},
		},
		{
			name: "reset after restored summary and remaining history",
			reset: &runtimeReset{
				afterMessages: 2,
			},
			compaction: &Compaction{
				Summary:       "earlier work",
				AfterMessages: 1,
			},
			want: []string{
				"Summary of earlier conversation:\n\nearlier work",
				"old answer",
				runtimeResetNotice,
				"new work",
			},
		},
		{
			name: "reset immediately after restored summary",
			reset: &runtimeReset{
				afterMessages: 2,
			},
			compaction: &Compaction{
				Summary:       "earlier work",
				AfterMessages: 2,
			},
			want: []string{
				"Summary of earlier conversation:\n\nearlier work",
				runtimeResetNotice,
				"new work",
			},
		},
		{
			name: "new compaction includes reset boundary",
			reset: &runtimeReset{
				afterMessages: 2,
			},
			compaction: &Compaction{
				Summary:       "current state",
				AfterMessages: 3,
			},
			want: []string{
				"Summary of earlier conversation:\n\ncurrent state",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				dialog := gai.Dialog{
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock("old task"),
						},
					},
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							gai.TextBlock("old answer"),
						},
					},
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock("new work"),
						},
					},
				}
				before, err := json.Marshal(dialog)
				if err != nil {
					t.Fatal(err)
				}
				// A retry must see the same boundary, not consume a one-shot notice.
				for range 2 {
					working := test.reset.workingDialog(
						dialog,
						test.compaction,
					)
					var got []string
					for _, message := range working {
						got = append(
							got,
							message.Blocks[0].Content.String(),
						)
					}
					if !slices.Equal(
						got,
						test.want,
					) {
						t.Fatalf(
							"working context: got %q, want %q",
							got,
							test.want,
						)
					}
				}
				after, err := json.Marshal(dialog)
				if err != nil || string(before) != string(after) {
					t.Fatal("runtime notice changed the canonical transcript")
				}
			},
		)
	}
}
