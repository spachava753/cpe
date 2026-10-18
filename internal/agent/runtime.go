package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

const (
	starlarkToolName = "starlark_repl"
	bashToolName     = "bash"
)

// HostConfig defines the session scope of a tool runtime.
type HostConfig struct {
	Session SessionID
	Cwd     string
	MCP     MCPTools
	// Descriptions supplies optional plugin descriptions for MCP namespaces.
	Descriptions map[MCPServerID]string
}

// HostFactory creates session-owned tools; ctx bounds setup, not their lifetime.
type HostFactory func(context.Context, HostConfig) (Host, error)

// Host owns a session's code environment and model-facing tools.
// Callers serialize execution and join active calls before Close.
type Host interface {
	Tools() []gaiagent.Tool
	// Instructions returns supplemental system context for the host's tools.
	Instructions() string
	Close(context.Context) error
}

// LocalHost creates a Dyson sphere and bash tool with explicit local host access.
// Tools run with the user's privileges without approval or filesystem restrictions.
func LocalHost(ctx context.Context, config HostConfig) (Host, error) {
	stdlib := dyson.HostStdlibConfig(config.Cwd)
	stdlib.CommandRunner = localCommandRunner{}
	stdlib.HTTPClient = dyson.HostHTTPClient()
	return newHost(
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
	config  HostConfig
	sphere  *dyson.Sphere
	runner  dyson.CommandRunner
	tools   []gaiagent.Tool
	catalog toolCatalog
}

func newHost(ctx context.Context, config HostConfig, stdlib dyson.StdlibConfig) (*toolHost, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(config.Cwd) || config.MCP == nil || stdlib.CommandRunner == nil {
		return nil, errors.New("host requires an absolute cwd, MCP, and a command runner")
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
	globals := starlark.StringDict{
		compactName: starlark.NewBuiltin(
			compactName,
			compact,
		),
		"emit": starlark.NewBuiltin(
			"emit",
			h.emit,
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
	h.sphere = dyson.NewSphere(
		func(thread *starlark.Thread, text string) {
			current, ok := dyson.EvaluationContext(thread).Value(invocationKey{}).(*invocation)
			if !ok {
				panic("Starlark output outside an invocation")
			}
			if err := current.output.add(gai.TextBlock(text)); err != nil {
				thread.Cancel(err.Error())
			}
		},
		dyson.NewStdlib(stdlib),
		dyson.GlobalSet(globals),
	)
	h.tools = []gaiagent.Tool{
		{
			Definition: gai.Tool{
				Name:        starlarkToolName,
				Description: "Execute Starlark in a persistent Dyson session. Use print/emit for output and compact(summary) to replace working context. Discover MCP tools with search_tools, describe_tool, and describe_namespace. Errors do not undo side effects.",
				InputSchema: stringSchema("code"),
			},
			Handler: gaiagent.ToolHandlerFunc(h.execute),
		},
		{
			Definition: gai.Tool{
				Name:        bashToolName,
				Description: "Execute a bash command in the session cwd. Output is captured; nonzero exit status is returned to the model. No persistent shell state. Prefer this for simple CLI and file inspection operations.",
				InputSchema: stringSchema("command"),
			},
			Handler: gaiagent.ToolHandlerFunc(h.execute),
		},
	}
	return h, nil
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

func (h *toolHost) Tools() []gaiagent.Tool { return h.tools }

func (h *toolHost) Instructions() string { return h.catalog.instructions() }

func (h *toolHost) Close(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return h.sphere.Close()
}

func (h *toolHost) execute(ctx context.Context, request gaiagent.ToolRequest) (gai.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return gai.Message{}, errors.New("tool runtime is closed")
	}
	current := &invocation{}
	ctx = context.WithValue(
		ctx,
		invocationKey{},
		current,
	)
	var runErr error
	var exitCode int
	switch request.Call.Name {
	case starlarkToolName:
		code, ok := request.Call.Parameters["code"].(string)
		if !ok || code == "" {
			runErr = errors.New("code must be a nonempty string")
			break
		}
		runErr = h.sphere.Eval(
			ctx,
			code,
		)
	case bashToolName:
		command, ok := request.Call.Parameters["command"].(string)
		if !ok || command == "" {
			runErr = errors.New("command must be a nonempty string")
			break
		}
		var result dyson.CommandResult
		result, runErr = h.RunCommand(
			ctx,
			dyson.Command{
				Args: []string{
					bashToolName,
					"--noprofile",
					"--norc",
					"-c",
					command,
				},
				Dir:    h.config.Cwd,
				Stdin:  dyson.StreamDiscard,
				Stdout: dyson.StreamPipe,
				Stderr: dyson.StreamPipe,
			},
		)
		exitCode = result.ReturnCode
		for _, text := range []string{
			fmt.Sprintf(
				"exit_code: %d",
				result.ReturnCode,
			),
			"stdout:\n" + string(result.Stdout),
			"stderr:\n" + string(result.Stderr),
		} {
			runErr = errors.Join(
				runErr,
				current.output.add(gai.TextBlock(text)),
			)
		}
	default:
		return gai.Message{}, errors.New("unknown runtime tool")
	}
	runErr = errors.Join(
		runErr,
		current.output.err,
	)
	if runErr != nil {
		if err := current.output.add(gai.TextBlock("Execution error: " + runErr.Error())); err != nil {
			// A full collector already contains output; reserve its final block for the failure.
			current.output.blocks[len(current.output.blocks)-1] = gai.TextBlock("Execution failed; output limit reached.")
		}
	}
	if len(current.output.blocks) == 0 {
		current.output.blocks = append(
			current.output.blocks,
			gai.TextBlock("Execution completed without emitted output."),
		)
	}
	message := gai.ToolResultMessage(
		request.Block.ID,
		current.output.blocks...,
	)
	message.ToolResultError = runErr != nil || exitCode != 0
	return message, nil
}

// RunCommand is shared by direct bash and Dyson subprocess calls. It never gives
// a child the ACP streams. The runner owns waiting and cancellation of the process.
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

func (h *toolHost) emit(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"value",
		&value,
	); err != nil {
		return nil, err
	}
	current, ok := dyson.EvaluationContext(thread).Value(invocationKey{}).(*invocation)
	if !ok {
		return nil, errors.New("emit outside an invocation")
	}
	var block gai.Block
	if content, ok := value.(*mcpContentValue); ok {
		var err error
		block, err = contentBlock(content.content)
		if err != nil {
			return nil, err
		}
	} else if text, ok := starlark.AsString(value); ok {
		block = gai.TextBlock(text)
	} else {
		return nil, errors.New("emit expects text or an MCP content item")
	}
	if err := current.output.add(block); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// Keep code output separate from programmatic MCP results.
const maxOutputBytes = 8 << 20
const maxTextBytes = 128 << 10

type outputCollector struct {
	blocks []gai.Block
	bytes  int
	err    error
}

func (o *outputCollector) add(block gai.Block) error {
	if o.err != nil {
		return o.err
	}
	size := len(block.Content.String())
	if block.ModalityType == gai.Text && size > maxTextBytes {
		block = gai.TextBlock(block.Content.String()[:maxTextBytes] + "\n[output truncated]")
		size = len(block.Content.String())
	}
	if len(o.blocks) >= 1024 || size > maxOutputBytes-o.bytes {
		o.err = errors.New("execution output limit exceeded; emit less content")
		return o.err
	}
	o.blocks = append(
		o.blocks,
		block,
	)
	o.bytes += size
	return nil
}
