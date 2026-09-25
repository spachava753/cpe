package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/skills"
)

// Options supplies all internal SDK dependencies. Generator can be a provider
// or a deterministic test generator. Tools are exposed only inside Starlark.
type Options struct {
	Config    config.Config
	Model     config.Model
	Generator gai.Generator
	Store     *session.Store
	CWD       string
	Tools     []repl.Tool
	Skills    skills.Catalog
}

const (
	// EventActivity reports the agent's current operation.
	EventActivity = "activity"
	// EventMessage carries a complete, durably saved message.
	EventMessage = "message"
	// EventDelta carries provisional streamed assistant text.
	EventDelta = "delta"
	// EventUsage carries cumulative session usage, including compaction requests.
	EventUsage = "usage"
	// EventModel carries a model selection applied at a generation boundary.
	EventModel      = "model"
	codeParameter   = "code"
	compactionEntry = "compaction"
)

// Event reports provisional text, accepted messages, or current activity.
type Event struct {
	Selection *ModelSelection
	Kind      string
	Text      string
	Message   *gai.Message
	Usage     *Usage
}

// Agent manages one durable conversation and interpreter.
type Agent struct {
	selectedName  string
	origins       map[int]messageOrigin
	selection     modelQueue
	opts          Options
	repl          *repl.REPL
	dialog        gai.Dialog
	emit          func(Event)
	compacted     bool
	pending       map[string]gai.Message
	usage         Usage
	contextSample *contextSample
	stateErr      error
}

// Open restores the interpreter and reconciles interrupted tool results.
func Open(ctx context.Context, opts Options) (*Agent, error) {
	if opts.Generator == nil || opts.Store == nil {
		return nil, errors.New("generator and session are required")
	}
	if err := opts.Model.ValidateBudget(); err != nil {
		return nil, err
	}
	a := &Agent{opts: opts}
	if err := a.restoreUsage(); err != nil {
		return nil, err
	}
	if err := a.restore(ctx); err != nil {
		return nil, err
	}
	return a, nil
}
func (a *Agent) restore(ctx context.Context) (err error) {
	// A durable head change must not leave a usable agent combining that head
	// with the previous branch's conversation or interpreter after failed replay.
	defer func() {
		if err != nil {
			a.stateErr = fmt.Errorf("agent restoration failed; reopen the session or select a checkpoint: %w", err)
		} else {
			a.stateErr = nil
		}
	}()
	if err := a.reload(); err != nil {
		return err
	}
	if a.repl != nil {
		err := a.repl.Close()
		a.repl = nil
		if err != nil {
			return err
		}
	}
	timeout, err := time.ParseDuration(a.opts.Config.Agent.ToolTimeout)
	if err != nil {
		return err
	}
	r, err := repl.New(ctx, repl.Options{Store: a.opts.Store, CWD: a.opts.CWD, Tools: a.opts.Tools, Timeout: timeout, OutputLimit: a.opts.Config.Agent.OutputLimit})
	if err != nil {
		return err
	}
	if err := a.reconcile(); err != nil {
		_ = r.Close()
		return err
	}
	a.repl = r
	return nil
}
func (a *Agent) reload() error {
	a.origins = make(map[int]messageOrigin)
	a.dialog = nil
	a.contextSample = nil
	for _, e := range a.opts.Store.Path() {
		if e.Type == EventUsage {
			var record usageRecord
			if err := json.Unmarshal(e.Data, &record); err != nil {
				return err
			}
			if record.Context != nil {
				a.contextSample = record.Context
			}
		}
		if e.Type != "message" && e.Type != compactionEntry {
			continue
		}
		m, origin, err := decodeMessage(e.Data)
		if err != nil {
			return err
		}
		if e.Type == compactionEntry {
			a.origins = make(map[int]messageOrigin)
			a.dialog = nil
			a.contextSample = nil
		}
		if origin != nil {
			a.origins[len(a.dialog)] = *origin
		}
		a.dialog = append(a.dialog, m)
	}
	return nil
}
func (a *Agent) append(m gai.Message) error { return a.appendWithOrigin(m, nil) }
func (a *Agent) appendWithOrigin(m gai.Message, origin *messageOrigin) error {
	record := encodeMessage(m)
	record.Origin = origin
	if _, err := a.opts.Store.Append("message", record); err != nil {
		return err
	}
	if origin != nil {
		a.origins[len(a.dialog)] = *origin
	}
	a.dialog = append(a.dialog, m)
	a.notify(Event{Kind: EventMessage, Message: &m})
	return nil
}
func (a *Agent) notify(e Event) {
	if a.emit != nil {
		a.emit(e)
	}
}
func resultMessage(r repl.Result) gai.Message {
	text := r.Output
	if r.Error != "" {
		text += "\n" + r.Error
	}
	if text == "" {
		text = "(no output)"
	}
	m := gai.ToolResultMessage(r.CallID, gai.TextBlock(text))
	for _, img := range r.Images {
		m.Blocks = append(m.Blocks, gai.Block{ID: r.CallID, BlockType: gai.Content, ModalityType: gai.Image, MimeType: img.MIMEType, Content: gai.Str(img.Data)})
	}
	m.ToolResultError = r.Error != ""
	return m
}
func (a *Agent) reconcile() error {
	results := map[string]repl.Result{}
	for _, e := range a.opts.Store.Path() {
		if e.Type == compactionEntry {
			// Providers may reuse call IDs after old messages leave the context.
			// Outcomes before this boundary cannot complete a new invocation.
			results = map[string]repl.Result{}
		}
		if e.Type == "eval_end" {
			var r repl.Result
			if err := json.Unmarshal(e.Data, &r); err != nil {
				return err
			}
			results[r.CallID] = r
		}
	}
	completed := map[string]bool{}
	for _, m := range a.dialog {
		if m.Role == gai.ToolResult {
			for _, b := range m.Blocks {
				completed[b.ID] = true
			}
		}
	}
	for _, m := range a.dialog {
		for _, b := range m.Blocks {
			if b.BlockType != gai.ToolCall || completed[b.ID] {
				continue
			}
			r, ok := results[b.ID]
			if !ok {
				r = repl.Result{CallID: b.ID, Error: "Tool call interrupted before a durable result. It was not retried."}
			}
			if err := a.append(resultMessage(r)); err != nil {
				return err
			}
			completed[b.ID] = true
		}
	}
	return nil
}
func (a *Agent) syncDialog(dialog gai.Dialog) error {
	if len(dialog) < len(a.dialog) {
		return errors.New("agent dialog regressed")
	}
	for i, m := range a.dialog {
		left, err := json.Marshal(encodeMessage(m))
		if err != nil {
			return err
		}
		right, err := json.Marshal(encodeMessage(dialog[i]))
		if err != nil {
			return err
		}
		if !bytes.Equal(left, right) {
			return fmt.Errorf("agent dialog diverged at message %d", i)
		}
	}
	for _, m := range dialog[len(a.dialog):] {
		if m.Role == gai.ToolResult && len(m.Blocks) > 0 {
			if saved, ok := a.pending[m.Blocks[0].ID]; ok {
				left, err := json.Marshal(encodeMessage(saved))
				if err != nil {
					return err
				}
				right, err := json.Marshal(encodeMessage(m))
				if err != nil {
					return err
				}
				if !bytes.Equal(left, right) {
					return errors.New("persisted tool result differs from accepted result")
				}
				delete(a.pending, m.Blocks[0].ID)
				a.dialog = append(a.dialog, m)
				continue
			}
		}
		if err := a.append(m); err != nil {
			return err
		}
	}
	return nil
}

// Prompt saves user input, runs the gai hook-driven loop, and creates a branch
// checkpoint. Cancellation preserves completed work and repairs pending results.
func (a *Agent) Prompt(ctx context.Context, text string, emit func(Event)) (finalErr error) {
	if a.stateErr != nil {
		return a.stateErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("empty prompt")
	}
	a.emit = emit
	defer func() {
		_, err := a.ApplyQueuedModel()
		finalErr = errors.Join(finalErr, err)
		a.emit = nil
	}()
	if _, err := a.ApplyQueuedModel(); err != nil {
		return err
	}
	expanded, err := a.opts.Skills.Expand(text)
	if err != nil {
		return err
	}
	input := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(expanded)}}
	prospective := append(append(gai.Dialog{}, a.dialog...), input)
	request := a.conversationRequest(prospective)
	compact, err := a.needsCompaction(request)
	if err != nil {
		return err
	}
	if compact {
		request = a.compactionRequest(prospective)
	}
	if err := a.checkContext(request); err != nil {
		return err
	}
	a.compacted = false
	a.pending = make(map[string]gai.Message)
	if err := a.append(input); err != nil {
		return err
	}
	definition := replDefinition()
	loop, err := gaiagent.New(gaiagent.Config{
		Generator: conversationGenerator{a}, Model: a.opts.Model.ID,
		Instructions: a.instructions(),
		Tools: []gaiagent.Tool{{Definition: definition, Handler: gaiagent.ToolHandlerFunc(func(ctx context.Context, req gaiagent.ToolRequest) (gai.Message, error) {
			code, ok := req.Call.Parameters[codeParameter].(string)
			if !ok {
				return gai.Message{}, errors.New("code must be a string")
			}
			a.notify(Event{Kind: EventActivity, Text: "Running Starlark"})
			result, err := a.repl.Eval(ctx, req.Block.ID, code)
			if err != nil {
				return gai.Message{}, err
			}
			return resultMessage(result), nil
		})}},
		PrepareDialog: gaiagent.PrepareDialogFunc(func(ctx context.Context, req gaiagent.PrepareDialogRequest) (gaiagent.PrepareDialogDecision, error) {
			if err := a.syncDialog(req.Request.Dialog); err != nil {
				return gaiagent.PrepareDialogDecision{}, err
			}
			if _, err := a.ApplyQueuedModel(); err != nil {
				return gaiagent.PrepareDialogDecision{}, err
			}
			decision, err := a.prepareContext(ctx, a.conversationRequest(req.Request.Dialog))
			if err != nil {
				return decision, err
			}
			if _, err := a.ApplyQueuedModel(); err != nil {
				return decision, err
			}
			return decision, a.checkContext(a.conversationRequest(a.dialog))
		}),
		BeforeGeneration: gaiagent.BeforeGenerationFunc(func(_ context.Context, req gaiagent.BeforeGenerationRequest) (gaiagent.BeforeGenerationDecision, error) {
			if req.Generation >= uint(a.opts.Config.Agent.MaxRounds) {
				return gaiagent.BeforeGenerationDecision{StopReason: "round_limit"}, nil
			}
			a.notify(Event{Kind: EventActivity, Text: "Thinking"})
			request := a.conversationRequest(req.Request.Dialog)
			return gaiagent.BeforeGenerationDecision{Request: &request}, a.checkContext(request)
		}),
		AfterGeneration: gaiagent.AfterGenerationFunc(func(_ context.Context, req gaiagent.AfterGenerationRequest) (*gai.Response, error) {
			if len(req.Response.Candidates) != 1 {
				return nil, errors.New("expected one candidate")
			}
			origin, err := a.originFor(req.Request)
			if err != nil {
				return nil, err
			}
			return nil, a.appendWithOrigin(req.Response.Candidates[0], origin)
		}),
		AfterTool: gaiagent.AfterToolFunc(func(_ context.Context, req gaiagent.AfterToolRequest) (gaiagent.AfterToolDecision, error) {
			m := gai.ToolResultMessage(req.Block.ID, req.Result.Blocks...)
			m.ToolResultError = req.Result.ToolResultError
			m.ExtraFields = req.Result.ExtraFields
			if _, err := a.opts.Store.Append("message", encodeMessage(m)); err != nil {
				return gaiagent.AfterToolDecision{}, err
			}
			a.pending[req.Block.ID] = m
			a.notify(Event{Kind: EventMessage, Message: &m})
			return gaiagent.AfterToolDecision{}, nil
		}),
	})
	if err != nil {
		return err
	}
	result, runErr := loop.Run(ctx, gaiagent.RunRequest{Dialog: a.dialog, Options: a.generationOptions()})
	// Hooks persist before tool execution. Reloading avoids losing a result when
	// cancellation prevents gai's AfterTool hook from being called.
	if runErr == nil {
		if err := a.syncDialog(result.Dialog); err != nil {
			return err
		}
	}
	if err := a.reload(); err != nil {
		return errors.Join(runErr, err)
	}
	if err := a.reconcile(); err != nil {
		return errors.Join(runErr, err)
	}
	if _, err := a.opts.Store.Append("checkpoint", map[string]string{"label": shortLabel(text)}); err != nil {
		return errors.Join(runErr, err)
	}
	if result.StopReason == "round_limit" {
		return errors.New("model round limit reached; send a message to continue")
	}
	return runErr
}

func replDefinition() gai.Tool {
	return gai.Tool{Name: "starlark_repl", Description: replDescription, InputSchema: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{codeParameter: {Type: "string", Description: "Starlark source. Use print() to return information."}}, Required: []string{codeParameter}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}}
}

func (a *Agent) instructions() gai.Message {
	return gai.SystemMessage(gai.TextBlock(a.opts.Config.System + "\n\n" + replDescription + repl.ToolInstructions(a.opts.Tools) + a.opts.Skills.Instructions()))
}

// Skills returns the immutable catalog used for discovery and user invocation.
func (a *Agent) Skills() skills.Catalog { return a.opts.Skills }

func shortLabel(text string) string {
	r := []rune(strings.ReplaceAll(text, "\n", " "))
	return string(r[:min(len(r), 80)])
}
func (a *Agent) generationOptions() gai.GenerationOptions {
	opts := gai.NewGenerationOptions()
	if a.opts.Model.ReasoningEffort != "" {
		gai.WithReasoningEffort(a.opts.Model.ReasoningEffort)(opts)
	}
	if a.opts.Model.MaxOutputTokens > 0 {
		gai.WithMaxGenerationTokens(a.opts.Model.MaxOutputTokens)(opts)
	}
	if a.opts.Model.Temperature != nil {
		gai.WithTemperature(*a.opts.Model.Temperature)(opts)
	}
	return opts
}

// Compact appends a durable summary without discarding any REPL history.
func (a *Agent) Compact(ctx context.Context) (finalErr error) {
	defer func() { _, err := a.ApplyQueuedModel(); finalErr = errors.Join(finalErr, err) }()
	if _, err := a.ApplyQueuedModel(); err != nil {
		return err
	}
	if a.stateErr != nil {
		return a.stateErr
	}
	if len(a.dialog) == 0 {
		return errors.New("nothing to compact")
	}
	a.notify(Event{Kind: EventActivity, Text: "Compacting"})
	request := a.compactionRequest(a.dialog)
	if err := a.checkContext(request); err != nil {
		return err
	}
	response, err := a.metered(compactionEntry).Generate(ctx, request)
	if err != nil {
		return err
	}
	var text strings.Builder
	if len(response.Candidates) == 1 {
		for _, b := range response.Candidates[0].Blocks {
			if b.BlockType == gai.Content && b.Content != nil {
				text.WriteString(b.Content.String())
			}
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return errors.New("compaction returned no summary")
	}
	m := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Conversation summary (the Starlark state is still available):\n" + text.String())}}
	if _, err := a.opts.Store.Append(compactionEntry, encodeMessage(m)); err != nil {
		return err
	}
	a.dialog = gai.Dialog{m}
	a.origins = make(map[int]messageOrigin)
	a.contextSample = nil
	_, err = a.opts.Store.Append("checkpoint", map[string]string{"label": "compacted"})
	return err
}

// Branch rebuilds interpreter and model context at a saved checkpoint. If replay
// fails after the head is saved, generation is disabled until another Branch
// succeeds or the session is reopened. Messages still reflects the selected head.
func (a *Agent) Branch(ctx context.Context, id string) error {
	if err := a.opts.Store.Branch(id); err != nil {
		return err
	}
	return a.restore(ctx)
}

// Messages returns the current dialog for read-only use between agent operations.
func (a *Agent) Messages() gai.Dialog { return a.dialog }

// Model returns the active model settings. Like all Agent methods, call it only
// between operations, never concurrently with generation or compaction.
func (a *Agent) Model() config.Model { return a.opts.Model }

// SetModel replaces generation settings and provider between operations, without
// rebuilding the interpreter or changing conversation history. Settings are local
// to this Agent; reopening a session uses the caller's configuration again.
func (a *Agent) SetModel(model config.Model, generator gai.Generator) error {
	if a.stateErr != nil {
		return a.stateErr
	}
	if err := checkModel(model, generator); err != nil {
		return err
	}
	a.opts.Model, a.opts.Generator = model, generator
	a.selectedName = ""
	return nil
}

// SetReasoningEffort changes subsequent turns and compaction between operations.
// Empty effort omits the option; invalid settings leave the active model intact.
func (a *Agent) SetReasoningEffort(effort string) error {
	if a.stateErr != nil {
		return a.stateErr
	}
	model, err := a.opts.Model.WithReasoningEffort(effort)
	if err != nil {
		return err
	}
	a.opts.Model = model
	return nil
}

// Checkpoints returns branchable entries, including the empty-session root.
func (a *Agent) Checkpoints() []session.Entry {
	var entries []session.Entry
	for _, e := range a.opts.Store.Entries() {
		if e.Type == "checkpoint" || e.Type == "session" {
			entries = append(entries, e)
		}
	}
	return entries
}

// SessionFile returns the JSONL path used by this agent.
func (a *Agent) SessionFile() string { return a.opts.Store.Filename() }

// Close releases interpreter resources. The caller still owns the Store.
func (a *Agent) Close() error {
	if a.repl == nil {
		return nil
	}
	return a.repl.Close()
}

const replDescription = `Use starlark_repl to execute persistent Starlarkx code. Variables and functions survive across calls and restarts. This is the only model tool. Print values to see output. The language is Starlark with top-level control flow, while loops, sets, and reassignment; it is not Python.
Use load("repl.star", "emit_image") to show images to the model. For MCP image content use emit_image(result["content"][i]); for base64 strings or raw bytes use emit_image(data, mime_type="image/png"). This attaches actual images to this tool result; printing base64 does not display an image. PNG, JPEG, GIF, and WebP are supported, up to 20 MiB of image bytes per evaluation. Emitted images persist even if later code fails, like printed output.
Load Dyson modules with load("os.star", "os"), load("requests.star", "requests"), and similarly glob, json, re, subprocess, time. Use open(path).read() to read text; open supports read modes only. Write via fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o644); os.write(fd, "text"); os.close(fd). Run commands with subprocess.run(["command", "arg"], capture_output=True, text=True). Use requests.get(url).text or .json() for HTTP.
Prefer closing files and descriptors within each chunk. Restored handles reattach by path and saved offset on NEW host I/O, without repeating creation or truncation; they observe the current filesystem. The working directory is fixed; use explicit paths or subprocess cwd. Environment mutation, process signaling, tempfile, pwd, grp, and shutil are not exposed.
Every host operation is journaled. Restoration supplies recorded results without repeating effects. A failed chunk rolls back Starlark state, but file writes, HTTP requests, and commands already performed remain. Inspect those effects before retrying. Historical data in restored variables is a snapshot; call the host again when fresh data is needed.`
