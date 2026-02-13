package agent

import (
	"context"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

func TestThinkingBlockFilter(t *testing.T) {
	tests := []struct {
		name               string
		keepGeneratorTypes []string
		inputDialog        gai.Dialog
	}{
		{
			name:               "keeps Anthropic thinking blocks when Anthropic is in keepGeneratorTypes",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("anthropic thinking..."),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorAnthropic},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "filters out Gemini thinking blocks when only Anthropic is allowed",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("gemini thinking..."),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorGemini},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "filters out thinking blocks without generator key",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Thinking, Content: gai.Str("thinking without generator key...")},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "filters out thinking blocks with nil ExtraFields",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Thinking, Content: gai.Str("thinking..."), ExtraFields: nil},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "preserves Content and ToolCall blocks unchanged",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Content, Content: gai.Str("text")},
						{BlockType: gai.ToolCall, Content: gai.Str("tool"), ID: "call_123"},
					},
				},
			},
		},
		{
			name:               "handles mixed conversation from multiple providers - keeps only Anthropic",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("gemini thinking"),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorGemini},
						},
						{BlockType: gai.Content, Content: gai.Str("gemini response")},
					},
				},
				{
					Role: gai.User,
					Blocks: []gai.Block{
						{BlockType: gai.Content, Content: gai.Str("user message")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("anthropic thinking"),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorAnthropic},
						},
						{BlockType: gai.Content, Content: gai.Str("anthropic response")},
					},
				},
			},
		},
		{
			name:               "keeps multiple generator types",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic, gai.ThinkingGeneratorGemini},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("gemini thinking"),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorGemini},
						},
						{BlockType: gai.Content, Content: gai.Str("gemini response")},
					},
				},
				{
					Role: gai.User,
					Blocks: []gai.Block{
						{BlockType: gai.Content, Content: gai.Str("user message")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("anthropic thinking"),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorAnthropic},
						},
						{BlockType: gai.Content, Content: gai.Str("anthropic response")},
					},
				},
				{
					Role: gai.User,
					Blocks: []gai.Block{
						{BlockType: gai.Content, Content: gai.Str("another user message")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("openrouter thinking"),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorOpenRouter},
						},
						{BlockType: gai.Content, Content: gai.Str("openrouter response")},
					},
				},
			},
		},
		{
			name:               "filters all thinking blocks when keepGeneratorTypes is empty",
			keepGeneratorTypes: []string{},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("anthropic thinking"),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorAnthropic},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "handles empty dialog",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog:        gai.Dialog{},
		},
		{
			name:               "handles message with no blocks",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role:   gai.Assistant,
					Blocks: []gai.Block{},
				},
			},
		},
		{
			name:               "filters OpenRouter thinking blocks when only Anthropic allowed",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorAnthropic},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("openrouter reasoning..."),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorOpenRouter},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "keeps Gemini thinking blocks with Gemini filter",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorGemini},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("gemini thinking..."),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorGemini},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "keeps OpenRouter thinking blocks with OpenRouter filter",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorOpenRouter},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("openrouter thinking..."),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorOpenRouter},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name:               "keeps Responses thinking blocks with Responses filter",
			keepGeneratorTypes: []string{gai.ThinkingGeneratorResponses},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("openai responses thinking..."),
							ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorResponses},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGenerator := &mockGaiGenerator{}
			filter := NewThinkingBlockFilter(mockGenerator, tt.keepGeneratorTypes)

			_, err := filter.Generate(context.Background(), tt.inputDialog, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
		})
	}
}

func TestThinkingBlockFilter_PreservesExtraFields(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewThinkingBlockFilter(mockGenerator, []string{gai.ThinkingGeneratorAnthropic})

	inputDialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, Content: gai.Str("hello")},
			},
			ExtraFields: map[string]interface{}{"cpe_message_id": "msg_abc123"},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:   gai.Thinking,
					Content:     gai.Str("thinking..."),
					ExtraFields: map[string]interface{}{gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorAnthropic},
				},
				{BlockType: gai.Content, Content: gai.Str("response")},
			},
			ExtraFields: map[string]interface{}{"cpe_message_id": "msg_def456"},
		},
	}

	_, err := filter.Generate(context.Background(), inputDialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, msg := range mockGenerator.capturedDialog {
		if msg.ExtraFields == nil {
			t.Errorf("message %d: ExtraFields is nil, expected it to be preserved", i)
			continue
		}
		id, ok := msg.ExtraFields["cpe_message_id"].(string)
		if !ok || id == "" {
			t.Errorf("message %d: expected cpe_message_id to be preserved, got %v", i, msg.ExtraFields)
		}
	}

	if id := mockGenerator.capturedDialog[0].ExtraFields["cpe_message_id"]; id != "msg_abc123" {
		t.Errorf("message 0: expected cpe_message_id 'msg_abc123', got %v", id)
	}
	if id := mockGenerator.capturedDialog[1].ExtraFields["cpe_message_id"]; id != "msg_def456" {
		t.Errorf("message 1: expected cpe_message_id 'msg_def456', got %v", id)
	}
}

func TestThinkingBlockFilter_PreservesToolResultError(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewThinkingBlockFilter(mockGenerator, []string{gai.ThinkingGeneratorAnthropic})

	inputDialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, Content: gai.Str("error message")},
			},
			ToolResultError: true,
		},
	}

	_, err := filter.Generate(context.Background(), inputDialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
}

func TestThinkingBlockFilter_Register(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewThinkingBlockFilter(mockGenerator, []string{gai.ThinkingGeneratorAnthropic})

	tool := gai.Tool{Name: "test_tool"}

	// mockGaiGenerator doesn't implement ToolRegister, so this should return an error
	err := filter.Register(tool)
	if err == nil {
		t.Error("expected error when underlying generator doesn't support tool registration")
	}
}

func TestConvenienceWrapperFunctions(t *testing.T) {
	tests := []struct {
		name           string
		wrapperFunc    gai.WrapperFunc
		expectedFilter func(*ThinkingBlockFilter) bool
	}{
		{
			name:        "WithAnthropicThinkingFilter",
			wrapperFunc: WithAnthropicThinkingFilter(),
			expectedFilter: func(f *ThinkingBlockFilter) bool {
				return len(f.keepGeneratorTypes) == 1 && f.keepGeneratorTypes[0] == gai.ThinkingGeneratorAnthropic
			},
		},
		{
			name:        "WithGeminiThinkingFilter",
			wrapperFunc: WithGeminiThinkingFilter(),
			expectedFilter: func(f *ThinkingBlockFilter) bool {
				return len(f.keepGeneratorTypes) == 1 && f.keepGeneratorTypes[0] == gai.ThinkingGeneratorGemini
			},
		},
		{
			name:        "WithOpenRouterThinkingFilter",
			wrapperFunc: WithOpenRouterThinkingFilter(),
			expectedFilter: func(f *ThinkingBlockFilter) bool {
				return len(f.keepGeneratorTypes) == 1 && f.keepGeneratorTypes[0] == gai.ThinkingGeneratorOpenRouter
			},
		},
		{
			name:        "WithResponsesThinkingFilter",
			wrapperFunc: WithResponsesThinkingFilter(),
			expectedFilter: func(f *ThinkingBlockFilter) bool {
				return len(f.keepGeneratorTypes) == 1 && f.keepGeneratorTypes[0] == gai.ThinkingGeneratorResponses
			},
		},
		{
			name:        "WithCerebrasThinkingFilter",
			wrapperFunc: WithCerebrasThinkingFilter(),
			expectedFilter: func(f *ThinkingBlockFilter) bool {
				return len(f.keepGeneratorTypes) == 1 && f.keepGeneratorTypes[0] == gai.ThinkingGeneratorCerebras
			},
		},
		{
			name:        "WithZaiThinkingFilter",
			wrapperFunc: WithZaiThinkingFilter(),
			expectedFilter: func(f *ThinkingBlockFilter) bool {
				return len(f.keepGeneratorTypes) == 1 && f.keepGeneratorTypes[0] == gai.ThinkingGeneratorZai
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGenerator := &mockGaiGenerator{}
			wrapped := tt.wrapperFunc(mockGenerator)

			filter, ok := wrapped.(*ThinkingBlockFilter)
			if !ok {
				t.Fatalf("expected *ThinkingBlockFilter, got %T", wrapped)
			}

			if !tt.expectedFilter(filter) {
				t.Errorf("filter does not have expected keepGeneratorTypes: %v", filter.keepGeneratorTypes)
			}
		})
	}
}

func TestWithThinkingBlockFilter_MultipleGenerators(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	wrapped := WithThinkingBlockFilter(gai.ThinkingGeneratorAnthropic, gai.ThinkingGeneratorGemini)(mockGenerator)

	filter, ok := wrapped.(*ThinkingBlockFilter)
	if !ok {
		t.Fatalf("expected *ThinkingBlockFilter, got %T", wrapped)
	}

	if len(filter.keepGeneratorTypes) != 2 {
		t.Errorf("expected 2 generator types, got %d", len(filter.keepGeneratorTypes))
	}

	if filter.keepGeneratorTypes[0] != gai.ThinkingGeneratorAnthropic {
		t.Errorf("expected first generator type to be %s, got %s", gai.ThinkingGeneratorAnthropic, filter.keepGeneratorTypes[0])
	}

	if filter.keepGeneratorTypes[1] != gai.ThinkingGeneratorGemini {
		t.Errorf("expected second generator type to be %s, got %s", gai.ThinkingGeneratorGemini, filter.keepGeneratorTypes[1])
	}
}
