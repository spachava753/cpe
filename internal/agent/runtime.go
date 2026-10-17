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
	// Plugins maps installed plugin names to their MCP server IDs; nil means none.
	Plugins map[string][]MCPServerID
	// ClientServers identifies MCP servers explicitly supplied by the ACP client.
	ClientServers []MCPServerID
}

// HostFactory creates session-owned tools; ctx bounds setup, not their lifetime.
type HostFactory func(context.Context, HostConfig) (Host, error)

// Host owns a session's code environment and model-facing tools.
// Callers serialize execution and join active calls before Close.
type Host interface {
	Tools() []gaiagent.Tool
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
	mu       sync.Mutex
	closed   bool
	config   HostConfig
	sphere   *dyson.Sphere
	runner   dyson.CommandRunner
	tools    []gaiagent.Tool
	mcpTools map[MCPServerID]*starlark.Dict
}

func newHost(ctx context.Context, config HostConfig, stdlib dyson.StdlibConfig) (*toolHost, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(config.Cwd) || config.MCP == nil || stdlib.CommandRunner == nil {
		return nil, errors.New("host requires an absolute cwd, MCP, and a command runner")
	}
	h := &toolHost{
		config:   config,
		runner:   stdlib.CommandRunner,
		mcpTools: make(map[MCPServerID]*starlark.Dict),
	}
	stdlib.CommandRunner = h
	clientTools := starlark.NewDict(len(config.ClientServers))
	for _, id := range config.ClientServers {
		functions, err := h.loadFunctions(
			ctx,
			id,
		)
		if err != nil {
			return nil, err
		}
		if err := clientTools.SetKey(
			starlark.String(id),
			functions,
		); err != nil {
			return nil, err
		}
	}
	clientTools.Freeze()
	globals := starlark.StringDict{
		"mcp_tools": clientTools,
		"load_plugin": starlark.NewBuiltin(
			"load_plugin",
			h.loadPlugin,
		),
		"emit": starlark.NewBuiltin(
			"emit",
			h.emit,
		),
		"mcp_servers": starlark.NewBuiltin(
			"mcp_servers",
			h.servers,
		),
		"describe_mcp_tools": starlark.NewBuiltin(
			"describe_mcp_tools",
			h.describeMCP,
		),
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
				Description: "Execute Starlark in a persistent session. Use Dyson modules for files, HTTP, and subprocesses. mcp_servers() lists configured servers without connecting. load_plugin(name) explicitly connects that plugin's servers and returns tool dictionaries keyed by server ID. Tool functions accept an arguments dictionary. ACP-client-supplied tools are already available in mcp_tools[server]. describe_mcp_tools(server) returns schemas after loading. Installed plugins stay disconnected until requested. Results expose content, structured_content, is_error. print emits text; emit(content_item) emits selected MCP text/images. Intermediate results are not shown to the model. Errors do not undo side effects or globals.",
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

func (h *toolHost) servers(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
	); err != nil {
		return nil, err
	}
	var ids []starlark.Value
	for _, id := range h.config.MCP.Servers() {
		ids = append(
			ids,
			starlark.String(id),
		)
	}
	return starlark.NewList(ids), nil
}

func (h *toolHost) describeMCP(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var server string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"server",
		&server,
	); err != nil {
		return nil, err
	}
	if _, loaded := h.mcpTools[MCPServerID(server)]; !loaded {
		return nil, errors.New("load the plugin with load_plugin before inspecting its server schemas")
	}
	tools, err := h.config.MCP.Load(
		dyson.EvaluationContext(thread),
		MCPServerID(server),
	)
	if err != nil {
		return nil, err
	}
	return toStarlark(
		thread,
		tools,
	)
}

func (h *toolHost) loadPlugin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
	servers, found := h.config.Plugins[name]
	if !found {
		return nil, fmt.Errorf(
			"unknown plugin %q",
			name,
		)
	}
	loaded := starlark.NewDict(len(servers))
	for _, server := range servers {
		functions, err := h.loadFunctions(
			dyson.EvaluationContext(thread),
			server,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"load plugin %q server %q: %w",
				name,
				server,
				err,
			)
		}
		if err := loaded.SetKey(
			starlark.String(server),
			functions,
		); err != nil {
			return nil, err
		}
	}
	loaded.Freeze()
	return loaded, nil
}

func (h *toolHost) loadFunctions(ctx context.Context, server MCPServerID) (*starlark.Dict, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if functions, found := h.mcpTools[server]; found {
		return functions, nil
	}
	tools, err := h.config.MCP.Load(
		ctx,
		server,
	)
	if err != nil {
		return nil, err
	}
	//nolint:contextcheck // Bound callbacks use their invocation context.
	functions, err := h.bindMCP(
		string(server),
		tools,
	)
	if err != nil {
		return nil, err
	}
	functions.Freeze()
	h.mcpTools[server] = functions
	return functions, nil
}

func (h *toolHost) bindMCP(server string, tools []*mcp.Tool) (*starlark.Dict, error) {
	functions := starlark.NewDict(len(tools))
	for _, tool := range tools {
		call := starlark.NewBuiltin(
			tool.Name,
			func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
					MCPServerID(server),
					&mcp.CallToolParams{
						Name:      tool.Name,
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
			},
		)
		if err := functions.SetKey(
			starlark.String(tool.Name),
			call,
		); err != nil {
			return nil, err
		}
	}
	return functions, nil
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
