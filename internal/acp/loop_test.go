package acp

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"text/template"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

type resetCountingCallback struct {
	resets int
}

func (c *resetCountingCallback) Call(context.Context, map[string]any) (gai.Message, error) {
	return gai.Message{}, nil
}

func (c *resetCountingCallback) Reset() {
	c.resets++
}

type executionMessageCapturingCallback struct {
	messageID string
}

func (c *executionMessageCapturingCallback) Call(ctx context.Context, _ map[string]any) (gai.Message, error) {
	c.messageID = xctx.ExecutionMessageIDFrom(ctx)
	return gai.ToolResultMessage("", gai.TextBlock("observed")), nil
}

type sessionUpdateFunc func(context.Context, *acp.SessionNotification) error

func (f sessionUpdateFunc) SessionUpdate(ctx context.Context, notification *acp.SessionNotification) error {
	return f(ctx, notification)
}

func TestLoopToolCallbackReceivesPersistedAssistantMessageID(t *testing.T) {
	t.Parallel()

	toolCall, err := gai.ToolCallBlock("probe-call", "history_probe", map[string]any{})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	generator := &testGen{responses: []genFunc{
		func(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
			return gai.Response{
				Candidates:   []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{toolCall}}},
				FinishReason: gai.ToolUse,
			}, nil
		},
		func(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
			return gai.Response{
				Candidates:   []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("finished")}}},
				FinishReason: gai.EndTurn,
			}, nil
		},
	}}
	callback := &executionMessageCapturingCallback{}
	store, _ := newTestSqlite(t)
	loop := Loop{
		G:     generator,
		Store: store,
		toolCallbacks: map[string]gai.ToolCallback{
			"history_probe": callback,
		},
		conn: sessionUpdateFunc(func(context.Context, *acp.SessionNotification) error { return nil }),
	}

	if _, err := loop.Generate(
		withSessionID(t.Context(), "test-session"),
		gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("inspect history")}}},
		nil,
	); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if callback.messageID == "" {
		t.Fatal("callback execution message ID is empty")
	}

	messages, err := store.GetMessages(t.Context(), []string{callback.messageID})
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}
	var stored gai.Message
	for message := range messages {
		stored = message
	}
	if stored.Role != gai.Assistant || len(stored.Blocks) != 1 || stored.Blocks[0].ID != "probe-call" {
		t.Fatalf("callback execution message = %#v, want persisted assistant tool-call message", stored)
	}
}

func TestLoopUsageSessionUpdate(t *testing.T) {
	tests := []struct {
		name     string
		model    config.Model
		metadata gai.Metadata
		wantUsed int
		wantCost *float64
	}{
		{
			name: "cache read with input cost",
			model: config.Model{
				ContextWindow:       200,
				InputCostPerMillion: new(2.0),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:     100,
				gai.UsageMetricCacheReadTokens: 40,
			},
			wantUsed: 100,
			wantCost: new(0.00012),
		},
		{
			name: "cache read and write with input cost",
			model: config.Model{
				ContextWindow:            200,
				InputCostPerMillion:      new(2.0),
				CacheReadCostPerMillion:  new(0.5),
				CacheWriteCostPerMillion: new(1.0),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      100,
				gai.UsageMetricCacheReadTokens:  40,
				gai.UsageMetricCacheWriteTokens: 10,
			},
			wantUsed: 100,
			wantCost: new(0.00013),
		},
		{
			name: "no model pricing omits cost",
			model: config.Model{
				ContextWindow: 200,
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      100,
				gai.UsageMetricGenerationTokens: 25,
				gai.UsageMetricCacheReadTokens:  40,
			},
			wantUsed: 125,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _ := newTestSqlite(t)
			if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
				Session: acp.SessionInfo{
					SessionID: "test-session",
					Cwd:       "/test/workspace",
					Title:     new("Test session"),
				},
				ModelRef: "test-model",
			}); err != nil {
				t.Fatalf("CreateACPSession: %v", err)
			}
			l := Loop{Cfg: config.Config{Model: tt.model}, Store: store}
			update, ok, err := l.usageSessionUpdate(t.Context(), "test-session", tt.metadata)
			if err != nil {
				t.Fatalf("usageSessionUpdate() err = %v, want nil", err)
			}
			if !ok {
				t.Fatal("usageSessionUpdate() ok = false, want true")
			}
			if update.SessionUpdate != acp.SessionUpdateTypeUsageUpdate {
				t.Fatalf("SessionUpdate = %q, want %q", update.SessionUpdate, acp.SessionUpdateTypeUsageUpdate)
			}
			if update.Size != uint64(tt.model.ContextWindow) {
				t.Fatalf("Size = %d, want %d", update.Size, tt.model.ContextWindow)
			}
			if update.Used != uint64(tt.wantUsed) {
				t.Fatalf("Used = %d, want %d", update.Used, tt.wantUsed)
			}

			if tt.wantCost == nil {
				if update.Cost != nil {
					t.Fatalf("Cost = %#v, want nil", update.Cost)
				}
				return
			}
			if update.Cost == nil {
				t.Fatal("Cost is nil")
			}
			if update.Cost.Currency != "USD" {
				t.Fatalf("Cost.Currency = %q, want USD", update.Cost.Currency)
			}
			if math.Abs(update.Cost.Amount-*tt.wantCost) > 0.0000000001 {
				t.Fatalf("Cost.Amount = %.12f, want %.12f", update.Cost.Amount, *tt.wantCost)
			}
		})
	}
}

// TestLoopUsageSessionUpdateCostAccumulatesAcrossLoops verifies that the
// reported cost is the session's cumulative total even when the Loop is
// recreated (new prompt, model switch, or process restart) since the total is
// persisted in SQLite rather than held in Loop memory.
func TestLoopUsageSessionUpdateCostAccumulatesAcrossLoops(t *testing.T) {
	store, _ := newTestSqlite(t)
	sessionID := acp.SessionId("test-session")
	for _, id := range []acp.SessionId{sessionID, "other-session"} {
		if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				SessionID: id,
				Cwd:       "/test/workspace",
				Title:     new("Test session"),
			},
			ModelRef: "test-model",
		}); err != nil {
			t.Fatalf("CreateACPSession(%q): %v", id, err)
		}
	}
	metadata := gai.Metadata{
		gai.UsageMetricInputTokens:      100,
		gai.UsageMetricGenerationTokens: 50,
	}

	first := Loop{
		Cfg: config.Config{Model: config.Model{
			ContextWindow:        200,
			InputCostPerMillion:  new(2.0),
			OutputCostPerMillion: new(4.0),
		}},
		Store: store,
	}
	update, ok, err := first.usageSessionUpdate(t.Context(), sessionID, metadata)
	if err != nil || !ok || update.SessionUpdate != acp.SessionUpdateTypeUsageUpdate || update.Cost == nil {
		t.Fatalf("first usageSessionUpdate() = %#v, %v, %v", update, ok, err)
	}
	// 100 * 2/1M + 50 * 4/1M
	wantFirst := 0.0004
	if math.Abs(update.Cost.Amount-wantFirst) > 0.0000000001 {
		t.Fatalf("first Cost.Amount = %.12f, want %.12f", update.Cost.Amount, wantFirst)
	}

	// a new Loop with different model pricing simulates a model switch,
	// which discards the previous runtime and its Loop
	second := Loop{
		Cfg: config.Config{Model: config.Model{
			ContextWindow:        200,
			InputCostPerMillion:  new(1.0),
			OutputCostPerMillion: new(1.0),
		}},
		Store: store,
	}
	update, ok, err = second.usageSessionUpdate(t.Context(), sessionID, metadata)
	if err != nil || !ok || update.SessionUpdate != acp.SessionUpdateTypeUsageUpdate || update.Cost == nil {
		t.Fatalf("second usageSessionUpdate() = %#v, %v, %v", update, ok, err)
	}
	// previous total plus 100 * 1/1M + 50 * 1/1M
	wantSecond := wantFirst + 0.00015
	if math.Abs(update.Cost.Amount-wantSecond) > 0.0000000001 {
		t.Fatalf("second Cost.Amount = %.12f, want %.12f", update.Cost.Amount, wantSecond)
	}

	// a different session must not see this session's cost
	other := Loop{Cfg: second.Cfg, Store: store}
	update, ok, err = other.usageSessionUpdate(t.Context(), "other-session", metadata)
	if err != nil || !ok || update.SessionUpdate != acp.SessionUpdateTypeUsageUpdate || update.Cost == nil {
		t.Fatalf("other session usageSessionUpdate() = %#v, %v, %v", update, ok, err)
	}
	if math.Abs(update.Cost.Amount-0.00015) > 0.0000000001 {
		t.Fatalf("other session Cost.Amount = %.12f, want %.12f", update.Cost.Amount, 0.00015)
	}
}

func TestLoopEffectiveGenOpts(t *testing.T) {
	tests := []struct {
		name        string
		modelType   string
		cfgParams   *gai.GenOpts
		override    *gai.GenOpts
		want        *gai.GenOpts
		wantSummary any
	}{
		{
			name: "both nil returns nil",
		},
		{
			name:      "config params apply when no override",
			cfgParams: &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "low"},
			want:      &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "low"},
		},
		{
			name:     "override applies when no config params",
			override: &gai.GenOpts{ThinkingBudget: "high"},
			want:     &gai.GenOpts{ThinkingBudget: "high"},
		},
		{
			name:      "override wins over config without dropping config fields",
			cfgParams: &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "low"},
			override:  &gai.GenOpts{ThinkingBudget: "high"},
			want:      &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "high"},
		},
		{
			name:        "responses with no generation opts requests detailed summary",
			modelType:   "responses",
			want:        &gai.GenOpts{},
			wantSummary: responses.ReasoningSummaryDetailed,
		},
		{
			name:        "responses thinking override requests detailed summary",
			modelType:   "responses",
			override:    &gai.GenOpts{ThinkingBudget: "high"},
			want:        &gai.GenOpts{ThinkingBudget: "high"},
			wantSummary: responses.ReasoningSummaryDetailed,
		},
		{
			name:        "responses thinking config requests detailed summary",
			modelType:   "responses",
			cfgParams:   &gai.GenOpts{ThinkingBudget: "low"},
			want:        &gai.GenOpts{ThinkingBudget: "low"},
			wantSummary: responses.ReasoningSummaryDetailed,
		},
		{
			name:        "responses without thinking requests detailed summary",
			modelType:   "responses",
			override:    &gai.GenOpts{MaxGenerationTokens: new(32000)},
			want:        &gai.GenOpts{MaxGenerationTokens: new(32000)},
			wantSummary: responses.ReasoningSummaryDetailed,
		},
		{
			name:      "responses preserves explicit summary request",
			modelType: "responses",
			override: &gai.GenOpts{
				ExtraArgs: map[string]any{
					gai.ResponsesThoughtSummaryDetailParam: responses.ReasoningSummaryConcise,
				},
			},
			want:        &gai.GenOpts{},
			wantSummary: responses.ReasoningSummaryConcise,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Loop{Cfg: config.Config{
				Model:            config.Model{Type: tt.modelType},
				GenerationParams: tt.cfgParams,
			}}
			got := l.effectiveGenOpts(tt.override)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("effectiveGenOpts() = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("effectiveGenOpts() is nil")
			}
			if got.ThinkingBudget != tt.want.ThinkingBudget {
				t.Fatalf("ThinkingBudget = %q, want %q", got.ThinkingBudget, tt.want.ThinkingBudget)
			}
			if tt.want.MaxGenerationTokens == nil {
				if got.MaxGenerationTokens != nil {
					t.Fatalf("MaxGenerationTokens = %d, want nil", *got.MaxGenerationTokens)
				}
			} else {
				if got.MaxGenerationTokens == nil {
					t.Fatal("MaxGenerationTokens is nil")
				}
				if *got.MaxGenerationTokens != *tt.want.MaxGenerationTokens {
					t.Fatalf("MaxGenerationTokens = %d, want %d", *got.MaxGenerationTokens, *tt.want.MaxGenerationTokens)
				}
			}

			gotSummary, gotSummaryOK := got.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]
			if tt.wantSummary == nil {
				if gotSummaryOK {
					t.Fatalf("responses thought summary = %#v, want unset", gotSummary)
				}
				return
			}
			if !gotSummaryOK {
				t.Fatalf("responses thought summary is unset, want %#v", tt.wantSummary)
			}
			if gotSummary != tt.wantSummary {
				t.Fatalf("responses thought summary = %#v, want %#v", gotSummary, tt.wantSummary)
			}
		})
	}
}

func TestLoopEffectiveGenOptsDoesNotMutateInputExtraArgs(t *testing.T) {
	extraArgs := map[string]any{"custom": "value"}
	override := &gai.GenOpts{
		ExtraArgs: extraArgs,
	}
	l := Loop{Cfg: config.Config{Model: config.Model{Type: "responses"}}}

	got := l.effectiveGenOpts(override)
	if got == nil {
		t.Fatal("effectiveGenOpts() is nil")
	}
	if got.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam] != responses.ReasoningSummaryDetailed {
		t.Fatalf(
			"responses thought summary = %#v, want %#v",
			got.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam],
			responses.ReasoningSummaryDetailed,
		)
	}
	if _, ok := override.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]; ok {
		t.Fatalf("override ExtraArgs was mutated: %#v", override.ExtraArgs)
	}
	if override.ExtraArgs["custom"] != "value" {
		t.Fatalf("override custom ExtraArgs = %#v, want value", override.ExtraArgs["custom"])
	}
}

func TestLoopGenerateCommitsCompactionBeforePublishingCompletion(t *testing.T) {
	t.Parallel()

	const compactionCallID = "compact-call-1"
	completionErr := errors.New("completion update failed")
	callback := &resetCountingCallback{}
	store, _ := newTestSqlite(t)
	toolCall, err := gai.ToolCallBlock(compactionCallID, config.CompactionToolName, map[string]any{
		"summary": "state to preserve",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	gen := &testGen{responses: []genFunc{
		func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
			return gai.Response{
				Candidates: []gai.Message{{
					Role:   gai.Assistant,
					Blocks: []gai.Block{toolCall},
				}},
				FinishReason: gai.ToolUse,
			}, nil
		},
	}}
	var updates []acp.SessionUpdate
	loop := Loop{
		G:     gen,
		Store: store,
		Cfg: config.Config{Compaction: &config.CompactionConfig{
			MaxCompactions:         5,
			Tool:                   gai.Tool{Name: config.CompactionToolName},
			InitialMessageTemplate: template.Must(template.New("compaction").Parse(`compacted conversation: {{ index .ToolArguments "summary" }}`)),
		}},
		toolCallbacks: map[string]gai.ToolCallback{
			"stateful": callback,
		},
		conn: sessionUpdateFunc(func(ctx context.Context, notification *acp.SessionNotification) error {
			updates = append(updates, notification.Update)
			status := notification.Update.Status
			if notification.Update.ToolCallID == compactionCallID && status != nil && *status == acp.ToolCallStatusCompleted {
				return completionErr
			}
			return nil
		}),
	}

	got, err := loop.Generate(
		withSessionID(t.Context(), "test-session"),
		gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Hello")}}},
		nil,
	)
	if !errors.Is(err, completionErr) {
		t.Fatalf("Generate() error = %v, want completion update failure", err)
	}
	if len(got) != 1 || got[0].Role != gai.User || got[0].Blocks[0].Content.String() != "compacted conversation: state to preserve" {
		t.Fatalf("Generate() dialog = %#v, want persisted replacement root", got)
	}
	if loop.compactionRestarts != 1 {
		t.Fatalf("compactionRestarts = %d, want 1", loop.compactionRestarts)
	}
	if callback.resets != 1 {
		t.Fatalf("callback resets = %d, want 1", callback.resets)
	}

	compactionParentID, ok := got[0].ExtraFields[storage.MessageCompactionParentIDKey].(string)
	if !ok || compactionParentID == "" {
		t.Fatalf("replacement root compaction parent = %#v, want persisted result ID", got[0].ExtraFields)
	}
	parentDialog, err := storage.GetDialogForMessage(t.Context(), store, compactionParentID)
	if err != nil {
		t.Fatalf("GetDialogForMessage() error = %v", err)
	}
	compactionResult := parentDialog[len(parentDialog)-1]
	if compactionResult.Role != gai.ToolResult || compactionResult.ToolResultError || compactionResult.Blocks[0].ID != compactionCallID {
		t.Fatalf("compaction parent = %#v, want successful tool result", compactionResult)
	}

	for _, update := range updates {
		if update.SessionUpdate == acp.SessionUpdateTypeUserMessageChunk {
			t.Fatalf("updates = %#v, replacement root must not publish after completion failure", updates)
		}
	}
}

func TestLoopGeneratePanicsWhenCompactionRootPersistenceFails(t *testing.T) {
	t.Parallel()

	const compactionCallID = "compact-call-1"
	callback := &resetCountingCallback{}
	store, rawDB := newTestSqlite(t)
	if _, err := rawDB.ExecContext(t.Context(), `
		CREATE TRIGGER fail_compaction_root
		BEFORE INSERT ON messages
		WHEN NEW.compaction_parent_id IS NOT NULL
		BEGIN
			SELECT RAISE(ABORT, 'compaction root blocked');
		END
	`); err != nil {
		t.Fatalf("create compaction root trigger: %v", err)
	}
	toolCall, err := gai.ToolCallBlock(compactionCallID, config.CompactionToolName, map[string]any{
		"summary": "state to preserve",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	gen := &testGen{responses: []genFunc{
		func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
			return gai.Response{
				Candidates: []gai.Message{{
					Role:   gai.Assistant,
					Blocks: []gai.Block{toolCall},
				}},
				FinishReason: gai.ToolUse,
			}, nil
		},
	}}
	loop := Loop{
		G:     gen,
		Store: store,
		Cfg: config.Config{Compaction: &config.CompactionConfig{
			MaxCompactions:         5,
			Tool:                   gai.Tool{Name: config.CompactionToolName},
			InitialMessageTemplate: template.Must(template.New("compaction").Parse(`compacted conversation: {{ index .ToolArguments "summary" }}`)),
		}},
		toolCallbacks: map[string]gai.ToolCallback{
			"stateful": callback,
		},
		conn: sessionUpdateFunc(func(context.Context, *acp.SessionNotification) error { return nil }),
	}

	defer func() {
		recovered := recover()
		panicErr, ok := recovered.(error)
		if !ok || !strings.Contains(panicErr.Error(), "persist compaction replacement root") {
			t.Fatalf("Generate() panic = %#v, want replacement-root persistence error", recovered)
		}
		if loop.compactionRestarts != 0 {
			t.Fatalf("compactionRestarts = %d, want 0", loop.compactionRestarts)
		}
		if callback.resets != 0 {
			t.Fatalf("callback resets = %d, want 0", callback.resets)
		}
		var resultCount int
		if err := rawDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM messages WHERE role = 'tool_result'`).Scan(&resultCount); err != nil {
			t.Fatalf("count persisted tool results: %v", err)
		}
		if resultCount != 1 {
			t.Fatalf("persisted tool results = %d, want 1", resultCount)
		}
		var rootCount int
		if err := rawDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM messages WHERE compaction_parent_id IS NOT NULL`).Scan(&rootCount); err != nil {
			t.Fatalf("count persisted compaction roots: %v", err)
		}
		if rootCount != 0 {
			t.Fatalf("persisted compaction roots = %d, want 0", rootCount)
		}
	}()

	_, _ = loop.Generate(
		withSessionID(t.Context(), "test-session"),
		gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Hello")}}},
		nil,
	)
}

func TestLoopCompactionRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	inputSchema, err := (&jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"summary":   {Type: "string"},
			"keyPoints": {Type: "array", Items: &jsonschema.Schema{Type: "string"}},
		},
		Required: []string{"summary"},
	}).Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	callback := &resetCountingCallback{}
	loop := Loop{
		Cfg: config.Config{Compaction: &config.CompactionConfig{
			MaxCompactions:         1,
			Tool:                   gai.Tool{Name: config.CompactionToolName},
			InputSchema:            inputSchema,
			InitialMessageTemplate: template.Must(template.New("compaction").Parse(`{{ range index .ToolArguments "keyPoints" }}{{ . }}{{ end }}`)),
		}},
		toolCallbacks: map[string]gai.ToolCallback{
			"stateful_tool": callback,
		},
	}
	toolCall, err := gai.ToolCallBlock("compact-invalid", config.CompactionToolName, map[string]any{
		"summary":   "state to preserve",
		"keyPoints": "should be an array",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}

	got, results, root, err := loop.compact(gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large history")}},
		{Role: gai.Assistant, Blocks: []gai.Block{toolCall}},
	})
	if err != nil {
		t.Fatalf("compact() error = %v", err)
	}
	if root != nil {
		t.Fatalf("compact() root = %#v, want nil", root)
	}
	if len(results) != 1 {
		t.Fatalf("compact() tool results = %d, want 1", len(results))
	}
	if len(got) != 3 {
		t.Fatalf("compact() dialog length = %d, want 3", len(got))
	}
	result := got[2]
	if result.Role != gai.ToolResult || !result.ToolResultError || len(result.Blocks) != 1 {
		t.Fatalf("compact() result = %#v, want failed tool result", result)
	}
	if result.Blocks[0].ID != "compact-invalid" {
		t.Fatalf("compact() tool result ID = %q, want compact-invalid", result.Blocks[0].ID)
	}
	if text := result.Blocks[0].Content.String(); !strings.Contains(text, "arguments do not match the input schema") || !strings.Contains(text, "keyPoints") {
		t.Fatalf("compact() tool error = %q, want schema error naming keyPoints", text)
	}
	if loop.compactionRetries != 1 {
		t.Fatalf("compactionRetries = %d, want 1", loop.compactionRetries)
	}
	if loop.compactionRestarts != 0 {
		t.Fatalf("compactionRestarts = %d, want 0", loop.compactionRestarts)
	}
	if callback.resets != 0 {
		t.Fatalf("callback resets = %d, want 0", callback.resets)
	}
}

func TestLoopCompactionRetryLimit(t *testing.T) {
	t.Parallel()

	inputSchema, err := (&jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"summary": {Type: "string"},
		},
		Required: []string{"summary"},
	}).Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	loop := Loop{Cfg: config.Config{Compaction: &config.CompactionConfig{
		MaxCompactions:         1,
		Tool:                   gai.Tool{Name: config.CompactionToolName},
		InputSchema:            inputSchema,
		InitialMessageTemplate: template.Must(template.New("compaction").Parse(`{{ index .ToolArguments "summary" }}`)),
	}}}
	toolCall, err := gai.ToolCallBlock("compact-invalid", config.CompactionToolName, map[string]any{
		"nextAction": "continue",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large history")}},
		{Role: gai.Assistant, Blocks: []gai.Block{toolCall}},
	}

	for attempt := 1; attempt <= 3; attempt++ {
		got, results, root, err := loop.compact(dialog)
		if err != nil {
			t.Fatalf("compact() attempt %d error = %v", attempt, err)
		}
		if root != nil {
			t.Fatalf("compact() attempt %d root = %#v, want nil", attempt, root)
		}
		if len(got) != 3 || len(results) != 1 || !results[0].ToolResultError {
			t.Fatalf("compact() attempt %d = %#v, %#v; want one recoverable tool error", attempt, got, results)
		}
	}

	got, results, root, err := loop.compact(dialog)
	if err == nil {
		t.Fatal("compact() fourth attempt error is nil, want retry-limit error")
	}
	if root != nil {
		t.Fatalf("compact() fourth attempt root = %#v, want nil", root)
	}
	if !strings.Contains(err.Error(), "maximum compaction retries exceeded") {
		t.Fatalf("compact() fourth attempt error = %q, want retry-limit context", err)
	}
	if len(got) != 3 || len(results) != 1 || !results[0].ToolResultError {
		t.Fatalf("compact() fourth attempt = %#v, %#v; want terminal tool error", got, results)
	}
	if text := results[0].Blocks[0].Content.String(); !strings.Contains(text, "retry limit of 3 exceeded") {
		t.Fatalf("compact() fourth attempt tool error = %q, want retry limit", text)
	}
	if loop.compactionRetries != 3 {
		t.Fatalf("compactionRetries = %d, want 3", loop.compactionRetries)
	}
}

func TestLoopCompactionCreatesToolErrorForTemplateFailure(t *testing.T) {
	t.Parallel()

	inputSchema, err := (&jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"summary": {Type: "string"},
		},
		Required: []string{"summary"},
	}).Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	callback := &resetCountingCallback{}
	loop := Loop{
		Cfg: config.Config{Compaction: &config.CompactionConfig{
			MaxCompactions:         1,
			Tool:                   gai.Tool{Name: config.CompactionToolName},
			InputSchema:            inputSchema,
			InitialMessageTemplate: template.Must(template.New("compaction").Parse(`{{ range index .ToolArguments "summary" }}{{ . }}{{ end }}`)),
		}},
		toolCallbacks: map[string]gai.ToolCallback{
			"stateful_tool": callback,
		},
	}
	toolCall, err := gai.ToolCallBlock("compact-template", config.CompactionToolName, map[string]any{
		"summary": "state",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}

	got, results, root, err := loop.compact(gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large history")}},
		{Role: gai.Assistant, Blocks: []gai.Block{toolCall}},
	})
	if err == nil {
		t.Fatal("compact() error is nil, want terminal template error")
	}
	if root != nil {
		t.Fatalf("compact() root = %#v, want nil", root)
	}
	if !strings.Contains(err.Error(), "rendering compaction initial message") {
		t.Fatalf("compact() error = %q, want template rendering context", err)
	}
	if len(results) != 1 {
		t.Fatalf("compact() tool results = %d, want 1", len(results))
	}
	if len(got) != 3 {
		t.Fatalf("compact() dialog length = %d, want 3", len(got))
	}
	result := got[2]
	if result.Role != gai.ToolResult || !result.ToolResultError || len(result.Blocks) != 1 {
		t.Fatalf("compact() result = %#v, want failed tool result", result)
	}
	if result.Blocks[0].ID != "compact-template" {
		t.Fatalf("compact() tool result ID = %q, want compact-template", result.Blocks[0].ID)
	}
	if text := result.Blocks[0].Content.String(); !strings.Contains(text, "could not render the configured initial message") || !strings.Contains(text, "do not retry compaction") {
		t.Fatalf("compact() tool error = %q, want terminal template execution error", text)
	}
	if loop.compactionRetries != 0 {
		t.Fatalf("compactionRetries = %d, want 0", loop.compactionRetries)
	}
	if loop.compactionRestarts != 0 {
		t.Fatalf("compactionRestarts = %d, want 0", loop.compactionRestarts)
	}
	if callback.resets != 0 {
		t.Fatalf("callback resets = %d, want 0", callback.resets)
	}
}

func TestLoopCompactionClearsStarlarkREPLState(t *testing.T) {
	t.Parallel()

	callback := &codemode.StarlarkREPLCallback{MaxTimeout: 5}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             "answer = 42\nprint(answer)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("initial Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "42\n" {
		t.Fatalf("initial Call() = %#v, want persisted answer", msg)
	}

	initialMessage := template.Must(template.New("compaction").Parse("compacted"))
	toolCall, err := gai.ToolCallBlock("compact-1", "compact_conversation", map[string]any{
		"summary": "retain the important context",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	store, _ := newTestSqlite(t)
	gen := &testGen{responses: []genFunc{
		func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
			return gai.Response{
				Candidates:   []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{toolCall}}},
				FinishReason: gai.ToolUse,
			}, nil
		},
		func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
			return gai.Response{
				Candidates:   []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("continued")}}},
				FinishReason: gai.EndTurn,
			}, nil
		},
	}}
	loop := Loop{
		G:     gen,
		Store: store,
		Cfg: config.Config{Compaction: &config.CompactionConfig{
			MaxCompactions:         1,
			Tool:                   gai.Tool{Name: "compact_conversation"},
			InitialMessageTemplate: initialMessage,
		}},
		toolCallbacks: map[string]gai.ToolCallback{
			codemode.StarlarkREPLToolName: callback,
		},
		conn: sessionUpdateFunc(func(context.Context, *acp.SessionNotification) error { return nil }),
	}
	if _, err := loop.Generate(
		withSessionID(t.Context(), "test-session"),
		gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large history")}}},
		nil,
	); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             "print(answer)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() after compaction error = %v", err)
	}
	if !msg.ToolResultError || len(msg.Blocks) != 1 || !strings.Contains(msg.Blocks[0].Content.String(), "undefined: answer") {
		t.Fatalf("Call() after compaction = %#v, want cleared REPL state", msg)
	}
}

func TestLoopCompactionPlansSuccessfulRebaseWithoutResettingState(t *testing.T) {
	t.Parallel()

	initialMessage := template.Must(template.New("compaction").Parse("compacted: {{ index .ToolArguments \"summary\" }}"))
	callback := &resetCountingCallback{}
	loop := Loop{
		Cfg: config.Config{Compaction: &config.CompactionConfig{
			MaxCompactions:         1,
			Tool:                   gai.Tool{Name: "compact_conversation"},
			InitialMessageTemplate: initialMessage,
		}},
		toolCallbacks: map[string]gai.ToolCallback{
			"starlark_repl": callback,
		},
	}
	toolCall, err := gai.ToolCallBlock("compact-1", "compact_conversation", map[string]any{
		"summary": "state to keep",
	})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}

	loop.compactionRetries = 2
	history, results, root, err := loop.compact(gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large history")}},
		{Role: gai.Assistant, Blocks: []gai.Block{toolCall}},
	})
	if err != nil {
		t.Fatalf("compact() error = %v", err)
	}
	if len(results) != 1 || results[0].ToolResultError {
		t.Fatalf("compact() tool results = %#v, want one successful result", results)
	}
	if len(history) != 3 || history[2].Role != gai.ToolResult || history[2].Blocks[0].ID != "compact-1" || history[2].Blocks[0].Content.String() != compactionSuccessText {
		t.Fatalf("compact() history = %#v, want persisted successful tool result", history)
	}
	if root == nil || len(root.Blocks) != 1 || root.Blocks[0].Content.String() != "compacted: state to keep" {
		t.Fatalf("compact() root = %#v", root)
	}
	if loop.compactionRetries != 2 {
		t.Fatalf("compactionRetries = %d, want unchanged value 2", loop.compactionRetries)
	}
	if loop.compactionRestarts != 0 {
		t.Fatalf("compactionRestarts = %d, want 0", loop.compactionRestarts)
	}
	if callback.resets != 0 {
		t.Fatalf("callback resets = %d, want 0 before replacement root is persisted", callback.resets)
	}
}
