package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	exactschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spachava753/dyson"
	starjson "github.com/spachava753/starlarkx/lib/json"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/xio"
)

// Runtime identifies the exact interpreter and host contract used by a journal.
const Runtime = "cpe-repl-1/dyson-b790356d9233/starlarkx-40a94bc8c78e/" + runtime.GOOS + "/" + runtime.GOARCH

// Tool is an injected host function. Execute accepts schema-validated JSON
// parameters and returns a JSON-serializable value. Calls are synchronous and
// journaled; implementations must honor cancellation and must not retain inputs.
// Input numbers, including nested numbers, are json.Number so integers do not
// lose precision. Callbacks choose the numeric range/conversion they require.
type Tool struct {
	Name        string
	Description string
	Schema      *jsonschema.Schema
	Execute     func(context.Context, map[string]any) (any, error)
}

// Options configures a single, non-concurrent REPL. Host is optional and exists
// for capability injection; nil enables the supported local host capabilities.
type Options struct {
	Store       *session.Store
	CWD         string
	Tools       []Tool
	Timeout     time.Duration
	OutputLimit int
	Host        *dyson.StdlibConfig
}

type toolArgumentFormat uint8

const (
	legacyToolArguments toolArgumentFormat = iota
	losslessToolArguments
)

type evalStart struct {
	CallID        string             `json:"callId"`
	Code          string             `json:"code"`
	OutputLimit   int                `json:"outputLimit"`
	ToolArguments toolArgumentFormat `json:"toolArguments,omitempty"`
}

// Result is the durable outcome of one chunk. An error rolls back interpreter
// state while preserving the audit trail and any already-performed host effects.
type Result struct {
	EvalID    string  `json:"evalId"`
	CallID    string  `json:"callId"`
	Output    string  `json:"output"`
	Error     string  `json:"error,omitempty"`
	Committed bool    `json:"committed"`
	Images    []Image `json:"images,omitempty"`
}

type chunk struct {
	id      string
	start   evalStart
	records []session.Entry
	end     *Result
}

// REPL owns one Dyson sphere and its durable host journal. Use Close when done.
type REPL struct {
	opts          Options
	j             *journal
	sphere        *dyson.Sphere
	output        *xio.TailBuffer
	images        []Image
	imageBytes    int
	tools         dyson.ModuleSet
	fatal         error
	toolArguments toolArgumentFormat
}

// New restores all committed chunks on the active branch without host effects.
// Interrupted chunks are recorded as failures; their host calls are never retried.
func New(ctx context.Context, opts Options) (*REPL, error) {
	if opts.Store == nil {
		return nil, errors.New("session store required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = time.Minute
	}
	if opts.OutputLimit <= 0 {
		opts.OutputLimit = 32000
	}
	r := &REPL{opts: opts, j: &journal{store: opts.Store}}
	tools, err := r.makeTools() //nolint:contextcheck // Callbacks use Dyson's active Eval context, not the construction context.
	if err != nil {
		return nil, err
	}
	r.tools = tools
	if err := r.restore(ctx); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func (r *REPL) newSphere() {
	host := dyson.HostStdlibConfig(r.opts.CWD)
	host.HTTPClient = dyson.HostHTTPClient()
	host.CommandRunner = dyson.HostCommandRunner()
	if r.opts.Host != nil {
		host = *r.opts.Host
	}
	cfg := dyson.StdlibConfig{
		FS:               &filesystem{host: host.FS, j: r.j},
		HTTPClient:       httpClient{host: host.HTTPClient, j: r.j},
		CommandRunner:    commandRunner{host: host.CommandRunner, j: r.j, cwd: r.opts.CWD},
		Clock:            clock{host: host.Clock, j: r.j},
		WorkingDirectory: directory(r.opts.CWD),
		Platform:         host.Platform,
	}
	source := dyson.NewStdlib(cfg).Select(dyson.StdlibSelection{
		Modules: []string{"os.star", "glob.star", "json.star", "re.star", "requests.star", "subprocess.star", "time.star"}, Globals: []string{"open"},
	})
	r.sphere = dyson.NewSphere(func(_ *starlark.Thread, text string) {
		if r.output != nil {
			_, _ = r.output.Write([]byte(text))
		}
	}, source, r.tools, dyson.ModuleSet{"repl.star": {"emit_image": starlark.NewBuiltin("emit_image", r.emitImage)}})
}

func (r *REPL) restore(ctx context.Context) error {
	r.j.active = false
	if r.sphere != nil {
		if err := r.sphere.Close(); err != nil {
			return err
		}
	}
	r.j.replay = true
	r.j.fatal = nil
	r.newSphere()
	var chunks []chunk
	var current *chunk
	for _, e := range r.opts.Store.Path() {
		switch e.Type {
		case "eval_start":
			if current != nil {
				return errors.New("nested evaluation in session")
			}
			current = &chunk{id: e.ID}
			if err := json.Unmarshal(e.Data, &current.start); err != nil {
				return err
			}
			if current.start.ToolArguments != legacyToolArguments && current.start.ToolArguments != losslessToolArguments {
				return fmt.Errorf("unsupported tool argument format %d", current.start.ToolArguments)
			}
		case "host_call", "host_result":
			if current == nil {
				return errors.New("host record outside evaluation")
			}
			current.records = append(current.records, e)
		case "eval_end":
			var result Result
			if err := json.Unmarshal(e.Data, &result); err != nil {
				return err
			}
			if current == nil || result.EvalID != current.id || result.CallID != current.start.CallID {
				return errors.New("mismatched evaluation outcome")
			}
			current.end = &result
			chunks = append(chunks, *current)
			current = nil
		}
	}
	if current != nil {
		result := Result{EvalID: current.id, CallID: current.start.CallID, Error: "Interrupted evaluation. Interpreter state was rolled back; external effects may have occurred. No host calls were retried. Inspect effects before repeating this code."}
		if _, err := r.opts.Store.Append("eval_end", result); err != nil {
			return err
		}
	}
	for _, c := range chunks {
		if !c.end.Committed {
			continue
		}
		r.j.records = c.records
		r.toolArguments = c.start.ToolArguments
		r.j.position = 0
		r.j.active = true
		limit := c.start.OutputLimit
		if limit <= 0 {
			limit = r.opts.OutputLimit
		}
		r.output = xio.NewTailBuffer(limit)
		r.images, r.imageBytes = nil, 0
		replayCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
		err := r.sphere.Eval(replayCtx, c.start.Code)
		cancel()
		r.j.active = false
		if r.j.fatal != nil {
			return fmt.Errorf("restore %s: %w", c.id, r.j.fatal)
		}
		if err != nil {
			return fmt.Errorf("restore %s: %w", c.id, err)
		}
		if r.j.position != len(c.records) {
			return fmt.Errorf("restore %s: unused host results", c.id)
		}
		output := r.outputText()
		// Older journals marked a single exact-capacity write as truncated even
		// when no bytes were lost. Accept that historical prefix only when all
		// retained bytes match; current evaluations use the corrected semantics.
		legacyExactLimit := c.start.ToolArguments == legacyToolArguments && !r.output.Truncated() && len(output) == limit && c.end.Output == truncatedOutputPrefix+output
		if output != c.end.Output && !legacyExactLimit {
			return fmt.Errorf("restore %s: output differs", c.id)
		}
		if !slices.Equal(r.images, c.end.Images) {
			return fmt.Errorf("restore %s: images differ", c.id)
		}
	}
	r.j.records = nil
	r.j.replay = false
	r.toolArguments = losslessToolArguments
	r.output = nil
	r.images, r.imageBytes = nil, 0
	return nil
}

const truncatedOutputPrefix = "[output truncated]\n"

func (r *REPL) outputText() string {
	output := r.output.String()
	if r.output.Truncated() {
		output = truncatedOutputPrefix + output
	}
	return output
}

// Eval syncs the input and every host outcome before committing the chunk.
// Infrastructure failures return an error and poison this REPL until reopened.
// Starlark errors are returned in Result.Error and permit subsequent chunks.
func (r *REPL) Eval(ctx context.Context, callID, code string) (Result, error) {
	if r.fatal != nil {
		return Result{}, r.fatal
	}
	if r.sphere == nil {
		return Result{}, errors.New("REPL is closed")
	}
	id, err := r.opts.Store.Append("eval_start", evalStart{CallID: callID, Code: code, OutputLimit: r.opts.OutputLimit, ToolArguments: losslessToolArguments})
	if err != nil {
		return Result{}, err
	}
	r.output = xio.NewTailBuffer(r.opts.OutputLimit)
	r.images, r.imageBytes = nil, 0
	r.toolArguments = losslessToolArguments
	r.j.active = true
	evalCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	evalErr := r.sphere.Eval(evalCtx, code)
	cancel()
	r.j.active = false
	if r.j.fatal != nil {
		r.fatal = r.j.fatal
		return Result{}, r.fatal
	}
	result := Result{EvalID: id, CallID: callID, Output: r.outputText(), Images: r.images, Committed: evalErr == nil}
	if evalErr != nil {
		result.Error = evalErr.Error() + "\nInterpreter state rolled back; external effects remain."
	}
	if _, err := r.opts.Store.Append("eval_end", result); err != nil {
		r.fatal = err
		return Result{}, err
	}
	if evalErr != nil {
		// Recovery uses its own bounded context so cancellation cannot prevent rollback.
		recoveryCtx, recoveryCancel := context.WithTimeout(context.WithoutCancel(ctx), r.opts.Timeout)
		err := r.restore(recoveryCtx)
		recoveryCancel()
		if err != nil {
			r.fatal = err
			return result, err
		}
	}
	return result, nil
}

// Close releases live handles without changing conversation history.
func (r *REPL) Close() error {
	r.j.active = false
	if r.sphere == nil {
		return nil
	}
	err := r.sphere.Close()
	r.sphere = nil
	return err
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (r *REPL) makeTools() (dyson.ModuleSet, error) {
	funcs := starlark.StringDict{}
	for _, tool := range r.opts.Tools {
		if !identifier.MatchString(tool.Name) || tool.Execute == nil {
			return nil, fmt.Errorf("invalid injected tool %q", tool.Name)
		}
		if _, exists := funcs[tool.Name]; exists {
			return nil, fmt.Errorf("duplicate injected tool %q", tool.Name)
		}
		schema := tool.Schema
		if schema == nil {
			schema = &jsonschema.Schema{Type: "object"}
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, err
		}
		// Legacy replay keeps its original validator. Compile the current one
		// lazily so stricter schema checks cannot prevent older chunks restoring.
		currentSchema := sync.OnceValues(func() (*exactschema.Schema, error) {
			return compileToolSchema(schema)
		})
		funcs[tool.Name] = starlark.NewBuiltin(tool.Name, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			params := starlark.NewDict(len(kwargs))
			if len(args) > 1 || len(args) == 1 && len(kwargs) > 0 {
				return nil, errors.New("pass one parameter dictionary or keyword arguments")
			}
			if len(args) == 1 {
				var ok bool
				params, ok = args[0].(*starlark.Dict)
				if !ok {
					return nil, errors.New("parameters must be a dictionary")
				}
			}
			for _, pair := range kwargs {
				if err := params.SetKey(pair[0], pair[1]); err != nil {
					return nil, err
				}
			}
			encoded, err := starlark.Call(thread, starjson.Module.Members["encode"], starlark.Tuple{params}, nil)
			if err != nil {
				return nil, err
			}
			text, ok := starlark.AsString(encoded)
			if !ok {
				return nil, errors.New("JSON encoder returned non-string")
			}
			var input map[string]any
			decoder := json.NewDecoder(strings.NewReader(text))
			if r.toolArguments == losslessToolArguments {
				decoder.UseNumber()
			}
			if err := decoder.Decode(&input); err != nil {
				return nil, err
			}
			if r.toolArguments == losslessToolArguments {
				schema, err := currentSchema()
				if err != nil {
					return nil, err
				}
				if err := schema.Validate(input); err != nil {
					return nil, err
				}
			} else if err := resolved.Validate(input); err != nil {
				return nil, err
			}
			value, err := boundary(r.j, "tool."+tool.Name, input, func() (json.RawMessage, error) {
				result, err := tool.Execute(dyson.EvaluationContext(thread), input)
				if err != nil {
					return nil, err
				}
				return json.Marshal(result)
			})
			if err != nil {
				return nil, err
			}
			return starlark.Call(thread, starjson.Module.Members["decode"], starlark.Tuple{starlark.String(value)}, nil)
		})
	}
	return dyson.ModuleSet{"tools.star": funcs}, nil
}

func compileToolSchema(schema *jsonschema.Schema) (*exactschema.Schema, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	document, err := exactschema.UnmarshalJSON(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	compiler := exactschema.NewCompiler()
	compiler.DefaultDraft(exactschema.Draft2020)
	// Tool schemas are supplied by the host. Resolving a reference must never
	// introduce unjournaled filesystem or network access during evaluation.
	compiler.UseLoader(nil)
	const location = "urn:cpe:tool-schema"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

// ToolInstructions describes the tools.star functions and their JSON schemas.
func ToolInstructions(tools []Tool) string {
	var b strings.Builder
	for _, t := range tools {
		schema, _ := json.Marshal(t.Schema)
		fmt.Fprintf(&b, "\nload(\"tools.star\", %q): %s\nParameters: %s\n", t.Name, t.Description, schema)
	}
	return b.String()
}
