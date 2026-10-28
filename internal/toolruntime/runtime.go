package toolruntime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/mcpclient"
)

const (
	// BashToolName is the model-facing name of the bash tool.
	BashToolName = "bash"
	// StarlarkToolName is the model-facing name of the StarlarkX tool.
	StarlarkToolName = "starlark_exec"

	bashDescription     = "Execute the generated bash script in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, the full output is saved to a temporary file, and filepath will be provided in the tool result. Optionally provide a timeout in seconds."
	starlarkDescription = "Execute StarlarkX code in the current working directory. Returns the text printed in the script. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, the full output is saved to a temporary file, and filepath will be provided in the tool result. Optionally provide a timeout in seconds."
)

// Config defines the session scope of a tool runtime.
type Config struct {
	// Catalog names the session's connected MCP servers and tools; it is fixed
	// for the session's lifetime.
	Catalog *mcpclient.Catalog
	// Cwd is the absolute working directory for commands and relative paths.
	Cwd string
	// TempDir receives overflow output; the runtime never deletes it.
	TempDir string
}

// Factory creates session-owned tools; ctx bounds setup, not their lifetime.
type Factory func(context.Context, Config) (Host, error)

// Host owns a session's model-facing tools. Calls may run concurrently; callers
// join active calls before Close.
type Host interface {
	Tools() []gaiagent.Tool
	Close(context.Context) error
}

type runtime struct {
	globals starlark.StringDict
	index   *searchIndex
	config  Config
	tools   []gaiagent.Tool
	mu      sync.Mutex
	closed  bool
}

// New creates the bash and starlark_exec tools. Both run with the user's full
// privileges, without approval prompts or sandboxing.
func New(ctx context.Context, config Config) (Host, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(config.Cwd) || !filepath.IsAbs(config.TempDir) || config.Catalog == nil {
		return nil, errors.New("tool runtime requires an absolute cwd and temporary directory, and an MCP catalog")
	}
	r := &runtime{
		config: config,
		index:  newSearchIndex(config.Catalog),
	}
	r.globals = r.builtins()
	r.tools = []gaiagent.Tool{
		{
			Definition: gai.Tool{
				Name:        BashToolName,
				Description: bashDescription,
				InputSchema: inputSchema(
					"script",
					"Bash script to execute",
				),
			},
			Handler: gaiagent.ToolHandlerFunc(r.bashTool),
		},
		{
			Definition: gai.Tool{
				Name:        StarlarkToolName,
				Description: starlarkDescription,
				InputSchema: inputSchema(
					"code",
					"StarlarkX code to execute",
				),
			},
			Handler: gaiagent.ToolHandlerFunc(r.starlarkTool),
		},
	}
	return r, nil
}

func inputSchema(field, description string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			field: {
				Type:        "string",
				Description: description,
			},
			"timeout": {
				Type:        "number",
				Description: "Timeout in seconds (optional, no default timeout)",
			},
		},
		Required: []string{
			field,
		},
		AdditionalProperties: &jsonschema.Schema{
			Not: &jsonschema.Schema{},
		},
	}
}

// Tools returns the bash and starlark_exec tools.
func (r *runtime) Tools() []gaiagent.Tool { return r.tools }

// Close rejects later calls. Overflow files remain for the model to read later.
func (r *runtime) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *runtime) open() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("tool runtime is closed")
	}
	return nil
}

// outputPattern names overflow files after the tool call that produced them.
func outputPattern(callID, kind string) string {
	safe := strings.Map(
		func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '_'
		},
		callID,
	)
	if len(safe) > 64 {
		safe = safe[:64]
	}
	return fmt.Sprintf(
		"%s-%s-*.txt",
		safe,
		kind,
	)
}

func errorResult(callID string, err error) gai.Message {
	message := gai.ToolResultMessage(
		callID,
		gai.TextBlock("Error: "+err.Error()),
	)
	message.ToolResultError = true
	return message
}

func (r *runtime) bashTool(ctx context.Context, request gaiagent.ToolRequest) (gai.Message, error) {
	if err := r.open(); err != nil {
		return gai.Message{}, err
	}
	callID := request.Block.ID
	script, _ := request.Call.Parameters["script"].(string)
	if strings.TrimSpace(script) == "" {
		return errorResult(
			callID,
			errors.New("script must be a nonempty string"),
		), nil
	}
	timeout, err := parseTimeout(request.Call.Parameters["timeout"])
	if err != nil {
		return errorResult(
			callID,
			err,
		), nil
	}
	sink := newTailSink(
		r.config.TempDir,
		outputPattern(
			callID,
			"bash",
		),
		toolMaxBytes,
		toolMaxLines,
	)
	run, err := runBash(
		ctx,
		r.config.Cwd,
		script,
		timeout,
		sink,
	)
	if ctx.Err() != nil {
		return gai.Message{}, ctx.Err()
	}
	if err != nil {
		return errorResult(
			callID,
			err,
		), nil
	}
	text := joinSections(
		run.output.notice(),
		run.output.text,
		run.diagnostics(),
	)
	if text == "" {
		text = "(no output)"
	}
	message := gai.ToolResultMessage(
		callID,
		gai.TextBlock(text),
	)
	message.ToolResultError = run.timedOut || run.exitCode != 0
	return message, nil
}
