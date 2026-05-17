package agent_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/nalgeon/be"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

// Update with: UPDATE_SNAPS=true go test ./internal/agent
var agentLoopSnapshots = snaps.WithConfig(snaps.Dir(".snapshots"), snaps.Raw())

func TestMain(m *testing.M) {
	code := m.Run()
	dirty, err := snaps.Clean(m)
	if err != nil {
		fmt.Println("Error cleaning snapshots:", err)
		os.Exit(1)
	}
	if code != 0 {
		os.Exit(code)
	}
	updateSnaps := os.Getenv("UPDATE_SNAPS")
	if dirty && updateSnaps != "true" && updateSnaps != "always" {
		fmt.Println("Some snapshots were outdated.")
		os.Exit(1)
	}
	os.Exit(0)
}

type agentLoopScriptedGeneration struct {
	response gai.Response
	err      error
}

type agentLoopScriptedModel struct {
	script []agentLoopScriptedGeneration
}

func (m *agentLoopScriptedModel) Generate(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
	if len(m.script) == 0 {
		return gai.Response{}, errors.New("no scripted generation response")
	}
	step := m.script[0]
	m.script = m.script[1:]
	return step.response, step.err
}

func (*agentLoopScriptedModel) Register(gai.Tool) error {
	return nil
}

type agentLoopToolFixture struct {
	tool     gai.Tool
	callback gai.ToolCallback
}

type toolCallbackFunc func(context.Context, json.RawMessage, string) (gai.Message, error)

func (f toolCallbackFunc) Call(ctx context.Context, params json.RawMessage, id string) (gai.Message, error) {
	return f(ctx, params, id)
}

func toolResultCallback(text string) gai.ToolCallback {
	return toolCallbackFunc(func(_ context.Context, _ json.RawMessage, id string) (gai.Message, error) {
		return gai.ToolResultMessage(id, gai.TextBlock(text)), nil
	})
}

func assistantText(text string) agentLoopScriptedGeneration {
	return agentLoopScriptedGeneration{
		response: gai.Response{
			FinishReason: gai.EndTurn,
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(text)}},
			},
		},
	}
}

func assistantToolUse(blocks ...gai.Block) agentLoopScriptedGeneration {
	return agentLoopScriptedGeneration{
		response: gai.Response{
			FinishReason: gai.ToolUse,
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: blocks},
			},
		},
	}
}

func mustToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, params)
	be.Err(t, err, nil)
	return block
}

// TODO: Add or expand snapshot scenarios that dump scripted model calls so the
// snapshots cover successful GenOpts pass-through, including non-default scalar
// fields, stop sequences, output modalities, extra args, and valid ToolChoice.
// TODO: Add an agent-loop snapshot path for Responses models that exercises the
// prompt-cache key and reasoning-summary opts injected for each model call. The
// focused unit coverage lives in genopts_test.go and generator_test.go today,
// but the replacement snapshot suite should cover the integrated loop behavior.

func TestAgentLoopSnapshotScenarios(t *testing.T) {
	inputCost := 2.0
	outputCost := 4.0
	cacheReadCost := 0.5
	cacheWriteCost := 1.0

	compactCfg := func(tokenThreshold, maxCompactions uint) config.Config {
		return config.Config{Compaction: &config.CompactionConfig{
			TokenThreshold: tokenThreshold,
			MaxCompactions: maxCompactions,
			Tool:           gai.Tool{Name: config.CompactionToolName, Description: "Compact the conversation"},
			InitialMessageTemplate: template.Must(template.New("agent-loop-compaction").Parse(
				`summary={{ index .ToolArguments "summary" }} parent={{ .PreviousLeafID }} tool={{ .CompactionToolName }}`,
			)),
		}}
	}

	saveDialog := func(t *testing.T, saver storage.DialogSaver, dialog gai.Dialog) gai.Dialog {
		t.Helper()

		savedDialog := slices.Clone(dialog)
		idx := 0
		for saved, err := range saver.SaveDialog(t.Context(), slices.Values(savedDialog)) {
			be.Err(t, err, nil)
			if err != nil {
				break
			}
			if idx >= len(savedDialog) {
				t.Fatalf("SaveDialog yielded too many messages")
			}
			savedDialog[idx] = saved
			idx++
		}
		if idx != len(savedDialog) {
			t.Fatalf("SaveDialog yielded %d messages, want %d", idx, len(savedDialog))
		}
		return savedDialog
	}

	tests := []struct {
		name            string
		cfg             config.Config
		initialDialog   gai.Dialog
		prepare         func(t *testing.T, saver storage.DialogSaver) gai.Dialog
		genOpts         *gai.GenOpts
		script          []agentLoopScriptedGeneration
		tools           []agentLoopToolFixture
		persist         bool
		wantErrContains string
	}{
		{
			name: "single assistant response persists dialog",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantText("hi there"),
			},
			persist: true,
		},
		{
			name: "single assistant response in incognito mode does not persist",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantText("hi there"),
			},
		},
		{
			name: "assistant response prints usage cost and persists metadata",
			cfg: config.Config{Model: config.Model{
				Ref:                      "test-model",
				ID:                       "provider-model-1",
				Type:                     "test-provider",
				DisplayName:              "Test Provider Model",
				ContextWindow:            100,
				InputCostPerMillion:      &inputCost,
				OutputCostPerMillion:     &outputCost,
				CacheReadCostPerMillion:  &cacheReadCost,
				CacheWriteCostPerMillion: &cacheWriteCost,
			}},
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("show usage")}},
			},
			script: []agentLoopScriptedGeneration{{
				response: gai.Response{
					FinishReason: gai.EndTurn,
					Candidates: []gai.Message{
						{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("usage tracked")}},
					},
					UsageMetadata: gai.Metadata{
						gai.UsageMetricInputTokens:      40,
						gai.UsageMetricGenerationTokens: 10,
						gai.UsageMetricCacheReadTokens:  5,
						gai.UsageMetricCacheWriteTokens: 3,
					},
				},
			}},
			persist: true,
		},
		{
			name: "tool callback executes and continues",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use lookup")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"})),
				assistantText("final answer"),
			},
			tools:   []agentLoopToolFixture{{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")}},
			persist: true,
		},
		{
			name: "multiple tool callbacks execute in order",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use tools")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(
					mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"}),
					mustToolCallBlock(t, "call_2", "read", map[string]any{"path": "README.md"}),
				),
				assistantText("combined answer"),
			},
			tools: []agentLoopToolFixture{
				{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")},
				{tool: gai.Tool{Name: "read"}, callback: toolResultCallback("read result")},
			},
			persist: true,
		},
		{
			name: "tool callback can return error tool result and continue",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use fragile tool")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "fragile", map[string]any{})),
				assistantText("recovered from tool error"),
			},
			tools: []agentLoopToolFixture{{
				tool: gai.Tool{Name: "fragile"},
				callback: toolCallbackFunc(func(_ context.Context, _ json.RawMessage, id string) (gai.Message, error) {
					msg := gai.ToolResultMessage(id, gai.TextBlock("recoverable tool failure"))
					msg.ToolResultError = true
					return msg, nil
				}),
			}},
			persist: true,
		},
		{
			name: "unknown tool returns an error after persisting tool call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use missing tool")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "missing", map[string]any{})),
			},
			persist:         true,
			wantErrContains: `tool 'missing' not found`,
		},
		{
			name: "nil callback terminates after tool call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("finish")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "finish", map[string]any{})),
			},
			tools:   []agentLoopToolFixture{{tool: gai.Tool{Name: "finish"}, callback: nil}},
			persist: true,
		},
		{
			name: "invalid tool parameters are returned as tool result",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("lookup with bad params")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": 123})),
				assistantText("asked for correction"),
			},
			tools: []agentLoopToolFixture{{
				tool: gai.Tool{Name: "lookup"},
				callback: toolCallbackFunc(func(_ context.Context, params json.RawMessage, id string) (gai.Message, error) {
					var input struct {
						Q string `json:"q"`
					}
					_ = json.Unmarshal(params, &input)
					if input.Q == "" {
						return gai.ToolResultMessage(id, gai.TextBlock("invalid parameters")), nil
					}
					return gai.ToolResultMessage(id, gai.TextBlock("lookup result")), nil
				}),
			}},
			persist: true,
		},
		{
			name: "tool callback error returns after persisted tool call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("lookup fails")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"})),
			},
			tools: []agentLoopToolFixture{{
				tool: gai.Tool{Name: "lookup"},
				callback: toolCallbackFunc(func(context.Context, json.RawMessage, string) (gai.Message, error) {
					return gai.Message{}, errors.New("lookup failed")
				}),
			}},
			persist:         true,
			wantErrContains: "lookup failed",
		},
		{
			name: "invalid tool result message fails before append",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("tool returns wrong id")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{})),
			},
			tools: []agentLoopToolFixture{{
				tool: gai.Tool{Name: "lookup"},
				callback: toolCallbackFunc(func(context.Context, json.RawMessage, string) (gai.Message, error) {
					return gai.ToolResultMessage("other_call", gai.TextBlock("wrong id")), nil
				}),
			}},
			persist:         true,
			wantErrContains: "invalid tool result message",
		},
		{
			name: "model error before assistant response persists pending dialog only",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("model fails")}},
			},
			script: []agentLoopScriptedGeneration{
				{err: errors.New("provider unavailable")},
			},
			persist:         true,
			wantErrContains: "provider unavailable",
		},
		{
			name: "model error with partial response prints but does not append",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("stream fails")}},
			},
			script: []agentLoopScriptedGeneration{{
				response: gai.Response{
					FinishReason: gai.EndTurn,
					Candidates: []gai.Message{
						{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("partial output")}},
					},
					UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 5, gai.UsageMetricGenerationTokens: 2},
				},
				err: errors.New("stream interrupted"),
			}},
			persist:         true,
			wantErrContains: "stream interrupted",
		},
		{
			name: "model error after tool result preserves tool result",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("tool then model fails")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{})),
				{err: errors.New("provider failed after tool")},
			},
			tools:           []agentLoopToolFixture{{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")}},
			persist:         true,
			wantErrContains: "provider failed after tool",
		},
		{
			name: "invalid model candidate count returns error",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("bad candidates")}},
			},
			script: []agentLoopScriptedGeneration{
				{response: gai.Response{FinishReason: gai.EndTurn}},
			},
			persist:         true,
			wantErrContains: "expected exactly one candidate",
		},
		{
			name: "invalid explicit tool choice fails before model call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("force missing tool")}},
			},
			genOpts:         &gai.GenOpts{ToolChoice: "missing"},
			persist:         true,
			wantErrContains: "tool 'missing' not found",
		},
		{
			name: "continuation appends to existing persisted dialog",
			prepare: func(t *testing.T, saver storage.DialogSaver) gai.Dialog {
				t.Helper()
				saved := saveDialog(t, saver, gai.Dialog{
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original question")}},
					{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("original answer")}},
				})
				return append(saved, gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}})
			},
			script: []agentLoopScriptedGeneration{
				assistantText("continued answer"),
			},
		},
		{
			name: "fork appends branch from existing root",
			prepare: func(t *testing.T, saver storage.DialogSaver) gai.Dialog {
				t.Helper()
				saved := saveDialog(t, saver, gai.Dialog{
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("root question")}},
					{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("first branch answer")}},
				})
				return gai.Dialog{
					saved[0],
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("alternate branch")}},
				}
			},
			script: []agentLoopScriptedGeneration{
				assistantText("alternate answer"),
			},
		},
		{
			name: "compaction warning is injected into next tool result",
			cfg:  compactCfg(10, 1),
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large conversation")}},
			},
			script: []agentLoopScriptedGeneration{{
				response: gai.Response{
					FinishReason: gai.ToolUse,
					Candidates: []gai.Message{{
						Role:   gai.Assistant,
						Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "lookup", map[string]any{})},
					}},
					UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 9, gai.UsageMetricGenerationTokens: 2},
				},
			}, assistantText("used warned result")},
			tools: []agentLoopToolFixture{
				{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")},
				{tool: gai.Tool{Name: config.CompactionToolName}, callback: nil},
			},
			persist: true,
		},
		{
			name: "compaction tool restarts into compacted branch",
			cfg:  compactCfg(0, 1),
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("start before compact")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", config.CompactionToolName, map[string]any{"summary": "condensed"})),
				assistantText("after compact"),
			},
			tools:   []agentLoopToolFixture{{tool: gai.Tool{Name: config.CompactionToolName}, callback: nil}},
			persist: true,
		},
		{
			name: "maximum compaction restarts returns error",
			cfg:  compactCfg(0, 1),
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("compact repeatedly")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", config.CompactionToolName, map[string]any{"summary": "first"})),
				assistantToolUse(mustToolCallBlock(t, "call_2", config.CompactionToolName, map[string]any{"summary": "second"})),
			},
			tools:           []agentLoopToolFixture{{tool: gai.Tool{Name: config.CompactionToolName}, callback: nil}},
			persist:         true,
			wantErrContains: "maximum compaction restarts exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var db *sql.DB
			var saver storage.DialogSaver
			if tt.persist || tt.prepare != nil {
				var err error
				db, err = sql.Open("sqlite3", ":memory:")
				be.Err(t, err, nil)
				db.SetMaxOpenConns(1)
				t.Cleanup(func() { _ = db.Close() })

				nextID := 0
				store, err := storage.NewSqlite(t.Context(), db, storage.WithIDGenerator(func() string {
					nextID++
					return fmt.Sprintf("msg_%03d", nextID)
				}))
				be.Err(t, err, nil)
				saver = store
			}

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			model := &agentLoopScriptedModel{script: tt.script}
			runtime := agent.NewRuntime(model, tt.cfg, saver, stdout, stderr, false)
			for _, fixture := range tt.tools {
				be.Err(t, runtime.Register(fixture.tool, fixture.callback), nil)
			}

			dialog := slices.Clone(tt.initialDialog)
			if tt.prepare != nil {
				dialog = tt.prepare(t, saver)
			}
			_, err := runtime.Generate(t.Context(), dialog, tt.genOpts)
			if tt.wantErrContains == "" {
				be.Err(t, err, nil)
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("Generate error = %v, want substring %q", err, tt.wantErrContains)
			}

			var snapshot strings.Builder
			fmt.Fprintf(&snapshot, "stdout: %q\n", stdout.String())
			fmt.Fprintf(&snapshot, "stderr: %q\n", stderr.String())
			snapshot.WriteString("sqlite:\n")
			if db == nil {
				snapshot.WriteString("<disabled>\n")
			} else {
				snapshot.WriteString(dumpSQLite(t, db))
			}
			agentLoopSnapshots.MatchStandaloneSnapshot(t, snapshot.String())
		})
	}
}

func dumpSQLite(t *testing.T, db *sql.DB) string {
	t.Helper()

	const nullValue = "NULL"

	quote := strconv.Quote
	nullString := func(value sql.NullString) string {
		if !value.Valid {
			return nullValue
		}
		return quote(value.String)
	}
	nullInt := func(value sql.NullInt64) string {
		if !value.Valid {
			return nullValue
		}
		return fmt.Sprint(value.Int64)
	}
	nullJSON := func(value sql.NullString) string {
		if !value.Valid || value.String == "" {
			return nullValue
		}
		decoder := json.NewDecoder(strings.NewReader(value.String))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return quote(value.String)
		}
		data, err := json.Marshal(decoded)
		if err != nil {
			return quote(value.String)
		}
		return quote(string(data))
	}

	var snapshot strings.Builder
	snapshot.WriteString("messages:\n")
	messageRows, err := db.QueryContext(t.Context(), `
		SELECT id,
		       parent_id,
		       compaction_parent_id,
		       role,
		       tool_result_error,
		       message_extra_fields,
		       model_ref,
		       model_id,
		       model_type,
		       model_display_name,
		       input_tokens,
		       output_tokens,
		       cache_read_tokens,
		       cache_write_tokens
		FROM messages
		ORDER BY rowid
	`)
	be.Err(t, err, nil)
	if err != nil {
		return snapshot.String()
	}
	defer messageRows.Close()
	for messageRows.Next() {
		var id, role string
		var toolResultError bool
		var parentID, compactionParentID, extraFields sql.NullString
		var modelRef, modelID, modelType, modelDisplayName sql.NullString
		var inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens sql.NullInt64
		if err := messageRows.Scan(
			&id,
			&parentID,
			&compactionParentID,
			&role,
			&toolResultError,
			&extraFields,
			&modelRef,
			&modelID,
			&modelType,
			&modelDisplayName,
			&inputTokens,
			&outputTokens,
			&cacheReadTokens,
			&cacheWriteTokens,
		); err != nil {
			t.Fatalf("scan messages snapshot row: %v", err)
		}
		fmt.Fprintf(
			&snapshot,
			"- id=%s parent_id=%s compaction_parent_id=%s role=%s tool_result_error=%t message_extra_fields=%s model_ref=%s model_id=%s model_type=%s model_display_name=%s input_tokens=%s output_tokens=%s cache_read_tokens=%s cache_write_tokens=%s\n",
			quote(id), nullString(parentID), nullString(compactionParentID), quote(role), toolResultError, nullJSON(extraFields), nullString(modelRef), nullString(modelID), nullString(modelType), nullString(modelDisplayName), nullInt(inputTokens), nullInt(outputTokens), nullInt(cacheReadTokens), nullInt(cacheWriteTokens),
		)
	}
	be.Err(t, messageRows.Err(), nil)

	snapshot.WriteString("blocks:\n")
	blockRows, err := db.QueryContext(t.Context(), `
		SELECT blocks.id,
		       blocks.message_id,
		       blocks.block_type,
		       blocks.modality_type,
		       blocks.mime_type,
		       blocks.content,
		       blocks.extra_fields,
		       blocks.sequence_order
		FROM blocks
		JOIN messages ON messages.id = blocks.message_id
		ORDER BY messages.rowid, blocks.sequence_order
	`)
	be.Err(t, err, nil)
	if err != nil {
		return snapshot.String()
	}
	defer blockRows.Close()
	for blockRows.Next() {
		var id, extraFields sql.NullString
		var messageID, blockType, mimeType, content string
		var modalityType, sequenceOrder int64
		if err := blockRows.Scan(&id, &messageID, &blockType, &modalityType, &mimeType, &content, &extraFields, &sequenceOrder); err != nil {
			t.Fatalf("scan blocks snapshot row: %v", err)
		}
		fmt.Fprintf(
			&snapshot,
			"- id=%s message_id=%s block_type=%s modality_type=%d mime_type=%s content=%s extra_fields=%s sequence_order=%d\n",
			nullString(id), quote(messageID), quote(blockType), modalityType, quote(mimeType), quote(content), nullJSON(extraFields), sequenceOrder,
		)
	}
	be.Err(t, blockRows.Err(), nil)
	return snapshot.String()
}
