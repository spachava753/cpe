package agent_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/nalgeon/be"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

// Update with: UPDATE_SNAPS=true go test ./internal/agent
var agentLoopSnapshots = snaps.WithConfig(snaps.Dir(".snapshots"), snaps.Ext(".md"), snaps.Raw())

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

type scriptedResponse struct {
	response gai.Response
	err      error
}

type scriptedModelCall struct {
	genOpts *gai.GenOpts
}

type scriptedGenerator struct {
	script []scriptedResponse
	calls  []scriptedModelCall
}

func (m *scriptedGenerator) Generate(_ context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	m.calls = append(m.calls, scriptedModelCall{genOpts: cloneGenOpts(opts)})

	if len(m.script) == 0 {
		return gai.Response{}, errors.New("no scripted generation response")
	}
	step := m.script[0]
	m.script = m.script[1:]

	resp := step.response
	inputTokens := 0
	for _, msg := range dialog {
		for _, block := range msg.Blocks {
			if block.Content != nil {
				inputTokens += len([]rune(block.Content.String()))
			}
		}
	}

	var hasAssistant bool
	outputTokens := 0
	for _, candidate := range resp.Candidates {
		if candidate.Role != gai.Assistant {
			continue
		}
		hasAssistant = true
		for _, block := range candidate.Blocks {
			if block.Content != nil {
				outputTokens += len([]rune(block.Content.String()))
			}
		}
	}
	if hasAssistant {
		resp.UsageMetadata = maps.Clone(resp.UsageMetadata)
		if resp.UsageMetadata == nil {
			resp.UsageMetadata = gai.Metadata{}
		}
		resp.UsageMetadata[gai.UsageMetricInputTokens] = inputTokens
		resp.UsageMetadata[gai.UsageMetricGenerationTokens] = outputTokens
	}

	return resp, step.err
}

func (*scriptedGenerator) Register(gai.Tool) error {
	return nil
}

type toolFixture struct {
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

func assistantText(text string) scriptedResponse {
	return scriptedResponse{
		response: gai.Response{
			FinishReason: gai.EndTurn,
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(text)}},
			},
		},
	}
}

func assistantToolUse(blocks ...gai.Block) scriptedResponse {
	return scriptedResponse{
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

func ptr[T any](v T) *T {
	return &v
}

func cloneGenOpts(opts *gai.GenOpts) *gai.GenOpts {
	if opts == nil {
		return nil
	}
	clone := *opts
	clone.StopSequences = slices.Clone(opts.StopSequences)
	clone.OutputModalities = slices.Clone(opts.OutputModalities)
	clone.ExtraArgs = maps.Clone(opts.ExtraArgs)
	return &clone
}

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
		script          []scriptedResponse
		tools           []toolFixture
		persist         bool
		wantErrContains string
	}{
		{
			name: "single assistant response persists dialog",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}},
			},
			script: []scriptedResponse{
				assistantText("hi there"),
			},
			persist: true,
		},
		{
			name: "single assistant response in incognito mode does not persist",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}},
			},
			script: []scriptedResponse{
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
			script: []scriptedResponse{{
				response: gai.Response{
					FinishReason: gai.EndTurn,
					Candidates: []gai.Message{
						{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("usage tracked")}},
					},
					UsageMetadata: gai.Metadata{
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
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"})),
				assistantText("final answer"),
			},
			tools:   []toolFixture{{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")}},
			persist: true,
		},
		{
			name: "multiple tool callbacks execute in order",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use tools")}},
			},
			script: []scriptedResponse{
				assistantToolUse(
					mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"}),
					mustToolCallBlock(t, "call_2", "read", map[string]any{"path": "README.md"}),
				),
				assistantText("combined answer"),
			},
			tools: []toolFixture{
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
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "fragile", map[string]any{})),
				assistantText("recovered from tool error"),
			},
			tools: []toolFixture{{
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
			script: []scriptedResponse{
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
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "finish", map[string]any{})),
			},
			tools:   []toolFixture{{tool: gai.Tool{Name: "finish"}, callback: nil}},
			persist: true,
		},
		{
			name: "invalid tool parameters are returned as tool result",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("lookup with bad params")}},
			},
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": 123})),
				assistantText("asked for correction"),
			},
			tools: []toolFixture{{
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
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"})),
			},
			tools: []toolFixture{{
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
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{})),
			},
			tools: []toolFixture{{
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
			script: []scriptedResponse{
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
			script: []scriptedResponse{{
				response: gai.Response{
					FinishReason: gai.EndTurn,
					Candidates: []gai.Message{
						{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("partial output")}},
					},
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
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{})),
				{err: errors.New("provider failed after tool")},
			},
			tools:           []toolFixture{{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")}},
			persist:         true,
			wantErrContains: "provider failed after tool",
		},
		{
			name: "invalid model candidate count returns error",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("bad candidates")}},
			},
			script: []scriptedResponse{
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
			name: "generation options pass through to every model call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("answer with configured generation parameters after lookup")}},
			},
			genOpts: &gai.GenOpts{
				Temperature:         ptr(0.37),
				TopP:                ptr(0.81),
				TopK:                ptr(uint(40)),
				FrequencyPenalty:    ptr(0.2),
				PresencePenalty:     ptr(0.4),
				N:                   ptr(uint(2)),
				MaxGenerationTokens: ptr(512),
				ToolChoice:          "lookup",
				StopSequences:       []string{"END", "STOP"},
				OutputModalities:    []gai.Modality{gai.Text, gai.Audio},
				AudioConfig:         gai.AudioConfig{VoiceName: "alloy", Format: "wav"},
				ThinkingBudget:      "medium",
				ExtraArgs: map[string]any{
					"provider_flag": true,
					"provider_mode": "strict",
				},
			},
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "generation parameters"})),
				assistantText("configured answer"),
			},
			tools:   []toolFixture{{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")}},
			persist: true,
		},
		{
			name: "continuation appends to existing persisted dialog",
			prepare: func(t *testing.T, saver storage.DialogSaver) gai.Dialog {
				t.Helper()
				saved := saveDialog(t, saver, gai.Dialog{
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original question")}},
					{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("original answer")}, ExtraFields: map[string]any{
						storage.AgentMetadataInputTokensKey:  int64(len([]rune("original question"))),
						storage.AgentMetadataOutputTokensKey: int64(len([]rune("original answer"))),
					}},
				})
				return append(saved, gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}})
			},
			script: []scriptedResponse{
				assistantText("continued answer"),
			},
		},
		{
			name: "fork appends branch from existing root",
			prepare: func(t *testing.T, saver storage.DialogSaver) gai.Dialog {
				t.Helper()
				saved := saveDialog(t, saver, gai.Dialog{
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("root question")}},
					{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("first branch answer")}, ExtraFields: map[string]any{
						storage.AgentMetadataInputTokensKey:  int64(len([]rune("root question"))),
						storage.AgentMetadataOutputTokensKey: int64(len([]rune("first branch answer"))),
					}},
				})
				return gai.Dialog{
					saved[0],
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("alternate branch")}},
				}
			},
			script: []scriptedResponse{
				assistantText("alternate answer"),
			},
		},
		{
			name: "compaction warning is injected into next tool result",
			cfg:  compactCfg(10, 1),
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("large conversation")}},
			},
			script: []scriptedResponse{{
				response: gai.Response{
					FinishReason: gai.ToolUse,
					Candidates: []gai.Message{{
						Role:   gai.Assistant,
						Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "lookup", map[string]any{})},
					}},
				},
			}, assistantText("used warned result")},
			tools: []toolFixture{
				{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("lookup result")},
				{tool: gai.Tool{Name: config.CompactionToolName}, callback: nil},
			},
			persist: true,
		},
		{
			name: "compaction tool restarts into compacted branch",
			cfg:  compactCfg(10, 1),
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Find the current project status and prepare a concise next-step plan.")}},
			},
			script: []scriptedResponse{
				{
					response: gai.Response{
						FinishReason: gai.ToolUse,
						Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{
							mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "project status"}),
							mustToolCallBlock(t, "call_2", "read", map[string]any{"path": "PLAN.md"}),
						}}},
					},
				},
				assistantToolUse(mustToolCallBlock(t, "call_3", config.CompactionToolName, map[string]any{
					"summary": "User asked for project status and a next-step plan. Lookup and PLAN.md were checked; continue with verification and final synthesis.",
				})),
				assistantToolUse(
					mustToolCallBlock(t, "call_4", "verify", map[string]any{"target": "project status"}),
					mustToolCallBlock(t, "call_5", "write_notes", map[string]any{"topic": "next steps"}),
				),
				assistantText("Project status is verified and the next-step plan is ready."),
			},
			tools: []toolFixture{
				{tool: gai.Tool{Name: "lookup"}, callback: toolResultCallback("status: implementation in progress")},
				{tool: gai.Tool{Name: "read"}, callback: toolResultCallback("PLAN.md: finish compaction scenario coverage")},
				{tool: gai.Tool{Name: "verify"}, callback: toolResultCallback("verification passed")},
				{tool: gai.Tool{Name: "write_notes"}, callback: toolResultCallback("notes written")},
				{tool: gai.Tool{Name: config.CompactionToolName}, callback: nil},
			},
			persist: true,
		},
		{
			name: "maximum compaction restarts returns error",
			cfg:  compactCfg(0, 1),
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("compact repeatedly")}},
			},
			script: []scriptedResponse{
				assistantToolUse(mustToolCallBlock(t, "call_1", config.CompactionToolName, map[string]any{"summary": "first"})),
				assistantToolUse(mustToolCallBlock(t, "call_2", config.CompactionToolName, map[string]any{"summary": "second"})),
			},
			tools:           []toolFixture{{tool: gai.Tool{Name: config.CompactionToolName}, callback: nil}},
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
			model := &scriptedGenerator{script: tt.script}
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
			writeShellSection(&snapshot, "stdout", stdout.String())
			writeShellSection(&snapshot, "stderr", stderr.String())
			dumpGenerationOptions(t, model.calls, &snapshot)
			dumpSQLite(t, db, &snapshot)
			agentLoopSnapshots.MatchStandaloneSnapshot(t, snapshot.String())
		})
	}
}

func writeShellSection(snapshot *strings.Builder, title, content string) {
	fmt.Fprintf(snapshot, "### %s\n\n````shell\n", title)
	snapshot.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		snapshot.WriteByte('\n')
	}
	snapshot.WriteString("````\n\n")
}

func dumpGenerationOptions(t *testing.T, calls []scriptedModelCall, snapshot *strings.Builder) {
	t.Helper()

	const nullValue = "NULL"

	floatPtr := func(value *float64) string {
		if value == nil {
			return nullValue
		}
		return strconv.FormatFloat(*value, 'g', -1, 64)
	}
	uintPtr := func(value *uint) string {
		if value == nil {
			return nullValue
		}
		return strconv.FormatUint(uint64(*value), 10)
	}
	intPtr := func(value *int) string {
		if value == nil {
			return nullValue
		}
		return strconv.Itoa(*value)
	}
	stringField := func(value string) string {
		if value == "" {
			return nullValue
		}
		return markdownTableCell(strconv.Quote(value))
	}
	jsonField := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			return markdownTableCell(strconv.Quote(fmt.Sprint(value)))
		}
		return markdownTableCell(string(data))
	}
	stringsField := func(values []string) string {
		if len(values) == 0 {
			return nullValue
		}
		return jsonField(values)
	}
	modalitiesField := func(values []gai.Modality) string {
		if len(values) == 0 {
			return nullValue
		}
		names := make([]string, len(values))
		for i, value := range values {
			names[i] = value.String()
		}
		return jsonField(names)
	}
	audioConfigField := func(value gai.AudioConfig) string {
		if value.VoiceName == "" && value.Format == "" {
			return nullValue
		}
		return jsonField(value)
	}
	extraArgsField := func(value map[string]any) string {
		if len(value) == 0 {
			return nullValue
		}
		return jsonField(value)
	}

	table := writeMarkdownTable(snapshot, "generation options", []string{
		"call",
		"temperature",
		"top_p",
		"top_k",
		"frequency_penalty",
		"presence_penalty",
		"n",
		"max_generation_tokens",
		"tool_choice",
		"stop_sequences",
		"output_modalities",
		"audio_config",
		"thinking_budget",
		"extra_args",
	})
	for i, call := range calls {
		if call.genOpts == nil {
			be.Err(t, table.Append([]string{
				strconv.Itoa(i + 1),
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
				nullValue,
			}), nil)
			continue
		}
		opts := call.genOpts
		be.Err(t, table.Append([]string{
			strconv.Itoa(i + 1),
			floatPtr(opts.Temperature),
			floatPtr(opts.TopP),
			uintPtr(opts.TopK),
			floatPtr(opts.FrequencyPenalty),
			floatPtr(opts.PresencePenalty),
			uintPtr(opts.N),
			intPtr(opts.MaxGenerationTokens),
			stringField(opts.ToolChoice),
			stringsField(opts.StopSequences),
			modalitiesField(opts.OutputModalities),
			audioConfigField(opts.AudioConfig),
			stringField(opts.ThinkingBudget),
			extraArgsField(opts.ExtraArgs),
		}), nil)
	}
	renderMarkdownTable(t, table, snapshot)
}

func dumpSQLite(t *testing.T, db *sql.DB, snapshot *strings.Builder) {
	t.Helper()

	const nullValue = "NULL"

	quote := func(value string) string {
		return markdownTableCell(strconv.Quote(value))
	}
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

	messageTable := writeMarkdownTable(snapshot, "messages", []string{
		"id",
		"parent_id",
		"compaction_parent_id",
		"role",
		"tool_result_error",
		"message_extra_fields",
		"model_ref",
		"model_id",
		"model_type",
		"model_display_name",
		"input_tokens",
		"output_tokens",
		"cache_read_tokens",
		"cache_write_tokens",
	})
	if db != nil {
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
		if err == nil {
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
				be.Err(t, messageTable.Append([]string{
					quote(id),
					nullString(parentID),
					nullString(compactionParentID),
					quote(role),
					fmt.Sprint(toolResultError),
					nullJSON(extraFields),
					nullString(modelRef),
					nullString(modelID),
					nullString(modelType),
					nullString(modelDisplayName),
					nullInt(inputTokens),
					nullInt(outputTokens),
					nullInt(cacheReadTokens),
					nullInt(cacheWriteTokens),
				}), nil)
			}
			be.Err(t, messageRows.Err(), nil)
		}
	}
	renderMarkdownTable(t, messageTable, snapshot)

	blockTable := writeMarkdownTable(snapshot, "blocks", []string{
		"id",
		"message_id",
		"block_type",
		"modality_type",
		"mime_type",
		"content",
		"extra_fields",
		"sequence_order",
	})
	if db != nil {
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
		if err == nil {
			defer blockRows.Close()
			for blockRows.Next() {
				var id, extraFields sql.NullString
				var messageID, blockType, mimeType, content string
				var modalityType, sequenceOrder int64
				if err := blockRows.Scan(&id, &messageID, &blockType, &modalityType, &mimeType, &content, &extraFields, &sequenceOrder); err != nil {
					t.Fatalf("scan blocks snapshot row: %v", err)
				}
				be.Err(t, blockTable.Append([]string{
					nullString(id),
					quote(messageID),
					quote(blockType),
					fmt.Sprint(modalityType),
					quote(mimeType),
					quote(content),
					nullJSON(extraFields),
					fmt.Sprint(sequenceOrder),
				}), nil)
			}
			be.Err(t, blockRows.Err(), nil)
		}
	}
	renderMarkdownTable(t, blockTable, snapshot)
}

func writeMarkdownTable(snapshot *strings.Builder, title string, headers []string) *tablewriter.Table {
	fmt.Fprintf(snapshot, "### %s\n\n", title)
	table := tablewriter.NewTable(snapshot,
		tablewriter.WithRenderer(renderer.NewMarkdown()),
		tablewriter.WithHeaderAutoFormat(tw.Off),
		tablewriter.WithRowAutoFormat(tw.Off),
		tablewriter.WithHeaderAlignment(tw.AlignLeft),
		tablewriter.WithRowAlignment(tw.AlignLeft),
	)
	table.Header(stringAnySlice(headers)...)
	return table
}

func renderMarkdownTable(t *testing.T, table *tablewriter.Table, snapshot *strings.Builder) {
	t.Helper()
	be.Err(t, table.Render(), nil)
	snapshot.WriteByte('\n')
}

func stringAnySlice(values []string) []any {
	cells := make([]any, len(values))
	for i, value := range values {
		cells[i] = value
	}
	return cells
}

func markdownTableCell(value string) string {
	value = strings.ReplaceAll(value, "\r\n", `\n`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, "|", `\|`)
}
