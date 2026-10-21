package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
	starjson "github.com/spachava753/starlarkx/lib/json"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"

	"github.com/spachava753/cpe/internal/mcpclient"
)

const starlarkToolName = "starlark"

// Config defines the session scope of a tool runtime.
type Config struct {
	Cwd string
	MCP MCPTools
	KV  KeyValueStore
	// Descriptions supplies optional plugin descriptions for MCP namespaces.
	Descriptions map[mcpclient.ServerID]string
}

// Factory creates session-owned tools; ctx bounds setup, not their lifetime.
type Factory func(context.Context, Config) (Host, error)

// Host owns a session's code environment and model-facing tools.
// Callers serialize execution and join active calls before Close.
type Host interface {
	Tools() []gaiagent.Tool
	// Instructions returns supplemental system context for the host's tools.
	Instructions() string
	Close(context.Context) error
}

// Local creates a Starlark tool with explicit local host access.
// Scripts run with the user's privileges without approval or filesystem restrictions.
func Local(ctx context.Context, config Config) (Host, error) {
	stdlib := dyson.HostStdlibConfig(config.Cwd)
	stdlib.CommandRunner = localCommandRunner{}
	stdlib.HTTPClient = dyson.HostHTTPClient()
	return New(
		ctx,
		config,
		stdlib,
	)
}

type invocationKey struct{}

type invocation struct {
	output outputCollector
}

type toolHost struct {
	mu      sync.Mutex
	closed  bool
	config  Config
	stdlib  dyson.StdlibConfig
	runner  dyson.CommandRunner
	tools   []gaiagent.Tool
	catalog toolCatalog
}

// New creates a runtime using the supplied filesystem, command runner, and standard library.
func New(ctx context.Context, config Config, stdlib dyson.StdlibConfig) (Host, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(config.Cwd) || config.MCP == nil || config.KV == nil || stdlib.CommandRunner == nil {
		return nil, errors.New("host requires an absolute cwd, MCP, KV store, and a command runner")
	}
	catalog, err := discoverTools(
		ctx,
		config.MCP,
		config.Descriptions,
	)
	if err != nil {
		return nil, err
	}
	h := &toolHost{
		config:  config,
		runner:  stdlib.CommandRunner,
		catalog: catalog,
	}
	stdlib.CommandRunner = h
	h.stdlib = stdlib
	h.tools = []gaiagent.Tool{
		{
			Definition: gai.Tool{
				Name:        starlarkToolName,
				Description: "Execute Starlark in a fresh runtime. Use kv.get, kv.store, and kv.keys for durable session data; print and image for output; compact(summary) to compact; edit(filename, old, new) for exact file edits. Discover MCP tools with search_tools, describe_tool, and describe_namespace. Errors do not undo effects.",
				InputSchema: stringSchema("code"),
			},
			Handler: gaiagent.ToolHandlerFunc(h.execute),
		},
	}
	return h, nil
}

func (h *toolHost) newSphere() *dyson.Sphere {
	globals := starlark.StringDict{
		"kv": h.kvModule(),
		"edit": starlark.NewBuiltin(
			"edit",
			h.edit,
		),
		compactName: starlark.NewBuiltin(
			compactName,
			compact,
		),
		"image": starlark.NewBuiltin(
			"image",
			image,
		),
		"search_tools": starlark.NewBuiltin(
			"search_tools",
			h.searchTools,
		),
		"describe_tool": starlark.NewBuiltin(
			"describe_tool",
			h.describeTool,
		),
		"describe_namespace": starlark.NewBuiltin(
			"describe_namespace",
			h.describeNamespace,
		),
	}
	for _, tool := range h.catalog.tools {
		globals[tool.Name] = starlark.NewBuiltin(
			tool.Name,
			//nolint:contextcheck // Calls use the script invocation context, not session setup.
			func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				return h.callMCP(
					thread,
					tool,
					fn,
					args,
					kwargs,
				)
			},
		)
	}
	return dyson.NewSphere(
		func(thread *starlark.Thread, text string) {
			current, ok := dyson.EvaluationContext(thread).Value(invocationKey{}).(*invocation)
			if !ok {
				panic("Starlark output outside an invocation")
			}
			current.output.add(gai.TextBlock(text))
		},
		dyson.NewStdlib(h.stdlib),
		dyson.GlobalSet(globals),
	)
}

func stringSchema(name string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			name: {
				Type: "string",
			},
		},
		Required: []string{
			name,
		},
		AdditionalProperties: &jsonschema.Schema{
			Not: &jsonschema.Schema{},
		},
	}
}

// Tools returns the model-facing Starlark tool.
func (h *toolHost) Tools() []gaiagent.Tool { return h.tools }

// Instructions returns MCP namespace guidance for the system prompt.
func (h *toolHost) Instructions() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.catalog.instructions()
}

// Close prevents further tool calls.
func (h *toolHost) Close(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

func (h *toolHost) execute(ctx context.Context, request gaiagent.ToolRequest) (gai.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return gai.Message{}, errors.New("tool runtime is closed")
	}
	current := &invocation{
		output: outputCollector{
			kv: h.config.KV,
		},
	}
	ctx = context.WithValue(
		ctx,
		invocationKey{},
		current,
	)
	if request.Call.Name != starlarkToolName {
		return gai.Message{}, errors.New("unknown runtime tool")
	}
	var runErr error
	code, ok := request.Call.Parameters["code"].(string)
	if !ok || code == "" {
		runErr = errors.New("code must be a nonempty string")
	} else {
		runErr = h.eval(
			ctx,
			code,
		)
	}
	blocks, runErr := current.output.finish(
		ctx,
		runErr,
	)
	if len(blocks) == 0 {
		blocks = append(
			blocks,
			gai.TextBlock("Execution completed without emitted output."),
		)
	}
	message := gai.ToolResultMessage(
		request.Block.ID,
		blocks...,
	)
	message.ToolResultError = runErr != nil
	return message, nil
}

// eval runs under the host lock so discovery and builtins use the same catalog snapshot.
func (h *toolHost) eval(ctx context.Context, code string) (err error) {
	catalog, err := discoverTools(
		ctx,
		h.config.MCP,
		h.config.Descriptions,
	)
	if err != nil {
		return err
	}
	h.catalog = catalog
	//nolint:contextcheck // Builtins receive the invocation context through Dyson at evaluation time.
	sphere := h.newSphere()
	defer func() {
		err = errors.Join(
			err,
			sphere.Close(),
		)
	}()
	return sphere.Eval(
		ctx,
		code,
	)
}

// RunCommand executes Dyson subprocess calls without giving children the ACP streams.
// The runner owns waiting and cancellation of the process.
func (h *toolHost) RunCommand(ctx context.Context, command dyson.Command) (dyson.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return dyson.CommandResult{}, err
	}
	if command.Dir == "" {
		command.Dir = h.config.Cwd
	}
	if !filepath.IsAbs(command.Dir) {
		command.Dir = filepath.Join(
			h.config.Cwd,
			command.Dir,
		)
	}
	if command.Stdin == dyson.StreamInherit {
		command.Stdin = dyson.StreamDiscard
	}
	if command.Stdout == dyson.StreamInherit {
		command.Stdout = dyson.StreamPipe
	}
	if command.Stderr == dyson.StreamInherit {
		command.Stderr = dyson.StreamPipe
	}
	return h.runner.RunCommand(
		ctx,
		command,
	)
}

func (h *toolHost) callMCP(thread *starlark.Thread, tool catalogTool, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	arguments := starlark.NewDict(0)
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"arguments?",
		&arguments,
	); err != nil {
		return nil, err
	}
	encoded, err := starlark.Call(
		thread,
		starjson.Module.Members["encode"],
		starlark.Tuple{
			arguments,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}
	text, ok := starlark.AsString(encoded)
	if !ok {
		return nil, errors.New("JSON encoder did not return a string")
	}
	result, err := h.config.MCP.Call(
		dyson.EvaluationContext(thread),
		tool.server,
		&mcp.CallToolParams{
			Name:      tool.remoteName,
			Arguments: json.RawMessage(text),
		},
	)
	if err != nil {
		return nil, err
	}
	return mcpResultValue(
		thread,
		result,
	)
}

func (h *toolHost) searchTools(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query, namespace string
	limit := 8
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"query",
		&query,
		"limit?",
		&limit,
		"namespace?",
		&namespace,
	); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("search limit must be positive")
	}
	return toStarlark(
		thread,
		h.catalog.search(
			query,
			limit,
			namespace,
		),
	)
}

func (h *toolHost) describeTool(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"name",
		&name,
	); err != nil {
		return nil, err
	}
	for _, tool := range h.catalog.tools {
		if tool.Name == name {
			return toStarlark(
				thread,
				tool,
			)
		}
	}
	return starlark.None, nil
}

func (h *toolHost) describeNamespace(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"name",
		&name,
	); err != nil {
		return nil, err
	}
	for _, namespace := range h.catalog.namespaces {
		if namespace.Name == name {
			return toStarlark(
				thread,
				namespace,
			)
		}
	}
	return starlark.None, nil
}

func toStarlark(thread *starlark.Thread, value any) (starlark.Value, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return starlark.Call(
		thread,
		starjson.Module.Members["decode"],
		starlark.Tuple{
			starlark.String(data),
		},
		nil,
	)
}

func mcpResultValue(thread *starlark.Thread, result *mcp.CallToolResult) (starlark.Value, error) {
	if result == nil {
		return nil, errors.New("MCP returned no result")
	}
	structured, err := toStarlark(
		thread,
		result.StructuredContent,
	)
	if err != nil {
		return nil, err
	}
	metadata, err := toStarlark(
		thread,
		result.Meta,
	)
	if err != nil {
		return nil, err
	}
	items := make(
		[]starlark.Value,
		0,
		len(result.Content),
	)
	for _, content := range result.Content {
		raw, err := toStarlark(
			thread,
			content,
		)
		if err != nil {
			return nil, err
		}
		items = append(
			items,
			&mcpContentValue{
				content: content,
				raw:     raw,
			},
		)
	}
	return starlarkstruct.FromStringDict(
		starlark.String("MCPResult"),
		starlark.StringDict{
			"content":            starlark.NewList(items),
			"structured_content": structured,
			"is_error":           starlark.Bool(result.IsError),
			"meta":               metadata,
		},
	), nil
}

func image(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Bytes
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"data",
		&data,
	); err != nil {
		return nil, err
	}
	current, ok := dyson.EvaluationContext(thread).Value(invocationKey{}).(*invocation)
	if !ok {
		return nil, errors.New("image outside an invocation")
	}
	block, err := imageBlock([]byte(data))
	if err != nil {
		return nil, err
	}
	current.output.add(block)
	return starlark.None, nil
}

// MCPTools retains SDK results for scripts; conversion to model content is separate.
type MCPTools interface {
	Servers() []mcpclient.ServerID
	Load(context.Context, mcpclient.ServerID) ([]*mcp.Tool, error)
	// Instructions returns initialization guidance for a loaded server.
	Instructions(mcpclient.ServerID) string
	Call(context.Context, mcpclient.ServerID, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}
