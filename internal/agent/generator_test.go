package agent

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
)

func TestApplyResponsesThinkingSummary(t *testing.T) {
	tests := []struct {
		name           string
		opts           *gai.GenOpts
		wantExtraArgs  bool
		wantSummaryVal any
	}{
		{
			name: "nil opts is safe",
			opts: nil,
		},
		{
			name: "empty thinking budget does nothing",
			opts: &gai.GenOpts{},
		},
		{
			name:           "sets detailed summary when thinking budget is set",
			opts:           &gai.GenOpts{ThinkingBudget: "high"},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name:           "sets detailed summary for medium budget",
			opts:           &gai.GenOpts{ThinkingBudget: "medium"},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name:           "sets detailed summary for low budget",
			opts:           &gai.GenOpts{ThinkingBudget: "low"},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name: "creates ExtraArgs map when nil",
			opts: &gai.GenOpts{
				ThinkingBudget: "high",
				ExtraArgs:      nil,
			},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name: "does not override existing summary param",
			opts: &gai.GenOpts{
				ThinkingBudget: "high",
				ExtraArgs: map[string]any{
					gai.ResponsesThoughtSummaryDetailParam: responses.ReasoningSummaryConcise,
				},
			},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryConcise,
		},
		{
			name: "preserves other ExtraArgs keys",
			opts: &gai.GenOpts{
				ThinkingBudget: "high",
				ExtraArgs: map[string]any{
					"other_key": "other_value",
				},
			},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture original ExtraArgs keys for preservation check
			var originalKeys map[string]any
			if tt.opts != nil && tt.opts.ExtraArgs != nil {
				originalKeys = make(map[string]any, len(tt.opts.ExtraArgs))
				for k, v := range tt.opts.ExtraArgs {
					originalKeys[k] = v
				}
			}

			ApplyResponsesThinkingSummary(tt.opts)

			if tt.opts == nil {
				return // nothing to check for nil opts
			}

			if tt.wantExtraArgs {
				if tt.opts.ExtraArgs == nil {
					t.Fatal("expected ExtraArgs to be non-nil")
				}
				val, exists := tt.opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]
				if !exists {
					t.Fatalf("expected %s in ExtraArgs", gai.ResponsesThoughtSummaryDetailParam)
				}
				if val != tt.wantSummaryVal {
					t.Errorf("expected summary param = %v, got %v", tt.wantSummaryVal, val)
				}

				// Verify other keys are preserved
				for k, v := range originalKeys {
					if k == gai.ResponsesThoughtSummaryDetailParam {
						continue
					}
					if tt.opts.ExtraArgs[k] != v {
						t.Errorf("ExtraArgs key %q was modified: want %v, got %v", k, v, tt.opts.ExtraArgs[k])
					}
				}
			} else {
				if tt.opts.ExtraArgs != nil {
					if _, exists := tt.opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]; exists {
						t.Errorf("did not expect %s in ExtraArgs", gai.ResponsesThoughtSummaryDetailParam)
					}
				}
			}
		})
	}
}
