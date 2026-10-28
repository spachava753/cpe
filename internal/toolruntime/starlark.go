package toolruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
	starjson "github.com/spachava753/starlarkx/lib/json"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"
	"github.com/spachava753/starlarkx/syntax"
)

// fileOptions make scripts read like ordinary Python while keeping recursion
// off: while loops are needed for pagination and polling.
var fileOptions = syntax.FileOptions{
	Set:             true,
	While:           true,
	TopLevelControl: true,
	GlobalReassign:  true,
}

const invocationLocal = "cpe.invocation"

// invocation is what a builtin sees of one starlark_exec call. Threads that
// parallel creates get their own cancellable context but share the call's state.
type invocation struct {
	ctx context.Context
	*callState
}

// callState is shared by every thread of one starlark_exec call.
type callState struct {
	output  *tailSink
	callID  string
	media   []gai.Block
	bashRun atomic.Int64
	mu      sync.Mutex
}

func (s *callState) addMedia(block gai.Block) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.media = append(
		s.media,
		block,
	)
}

func (s *callState) takeMedia() []gai.Block {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.media
}

func currentInvocation(thread *starlark.Thread) (*invocation, error) {
	current, ok := thread.Local(invocationLocal).(*invocation)
	if !ok {
		return nil, errors.New("builtin called outside a starlark_exec invocation")
	}
	return current, nil
}

// newThread creates a thread for an invocation. parallel uses extra threads so
// that concurrent host calls never share one thread's state.
func newThread(current *invocation) *starlark.Thread {
	thread := &starlark.Thread{
		Name: "starlark_exec",
		// StarlarkX's print already appends its end argument, a newline by default.
		Print: func(_ *starlark.Thread, message string) {
			_, _ = current.output.Write([]byte(message))
		},
	}
	thread.SetLocal(
		invocationLocal,
		current,
	)
	return thread
}

func (r *runtime) builtins() starlark.StringDict {
	members := make(starlark.StringDict)
	for _, server := range r.config.Catalog.Servers() {
		for _, tool := range server.Tools {
			members[tool.Name] = starlark.NewBuiltin(
				tool.Name,
				func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
					return callMCP(
						thread,
						tool,
						fn,
						args,
						kwargs,
					)
				},
			)
		}
	}
	tools := starlarkstruct.FromStringDict(
		starlark.String("tools"),
		members,
	)
	globals := starlark.StringDict{
		"tools": tools,
		"json":  starjson.Module,
	}
	for name, function := range map[string]func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error){
		"view":             view,
		"bash":             r.bashBuiltin,
		"read_file":        r.readFile,
		"write_file":       r.writeFile,
		"edit_file":        r.editFile,
		"search_tools":     r.searchTools,
		"describe_tool":    r.describeTool,
		"describe_server":  r.describeServer,
		"parallel":         r.parallel,
		compactBuiltinName: compact,
	} {
		globals[name] = starlark.NewBuiltin(
			name,
			function,
		)
	}
	globals.Freeze()
	return globals
}

func (r *runtime) starlarkTool(ctx context.Context, request gaiagent.ToolRequest) (gai.Message, error) {
	if err := r.open(); err != nil {
		return gai.Message{}, err
	}
	callID := request.Block.ID
	code, _ := request.Call.Parameters["code"].(string)
	if strings.TrimSpace(code) == "" {
		return errorResult(
			callID,
			errors.New("code must be a nonempty string"),
		), nil
	}
	timeout, err := parseTimeout(request.Call.Parameters["timeout"])
	if err != nil {
		return errorResult(
			callID,
			err,
		), nil
	}
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(
			ctx,
			timeout,
		)
		defer cancel()
	}
	current := &invocation{
		ctx: runCtx,
		callState: &callState{
			callID: callID,
			output: newTailSink(
				r.config.TempDir,
				outputPattern(
					callID,
					"starlark",
				),
				toolMaxBytes,
				toolMaxLines,
			),
		},
	}
	thread := newThread(current)
	// Cancelling the thread stops Starlark code; cancelling runCtx stops host calls.
	stop := context.AfterFunc(
		runCtx,
		func() { thread.Cancel("execution cancelled") },
	)
	_, execErr := starlark.ExecFileOptions(
		&fileOptions,
		thread,
		"script.star",
		code,
		r.globals,
	)
	stop()
	// Finishing closes any overflow file, including when the call was cancelled.
	output := current.output.finish()
	if ctx.Err() != nil {
		return gai.Message{}, ctx.Err()
	}
	var diagnostic string
	switch {
	case runCtx.Err() != nil:
		diagnostic = fmt.Sprintf(
			"[Timed out after %s; the script was stopped. Effects of statements that already ran remain.]",
			timeout,
		)
	case execErr != nil:
		diagnostic = "Error: " + formatError(execErr)
	}
	text := joinSections(
		output.notice(),
		output.text,
		diagnostic,
	)
	media := current.takeMedia()
	if text == "" && len(media) == 0 {
		text = "Script completed with no output."
	}
	var blocks []gai.Block
	if text != "" {
		blocks = append(
			blocks,
			gai.TextBlock(text),
		)
	}
	blocks = append(
		blocks,
		media...,
	)
	message := gai.ToolResultMessage(
		callID,
		blocks...,
	)
	message.ToolResultError = diagnostic != ""
	return message, nil
}

func formatError(err error) string {
	if evalErr, ok := errors.AsType[*starlark.EvalError](err); ok {
		return evalErr.Backtrace()
	}
	return err.Error()
}

// timeoutArg converts an optional Starlark timeout in seconds.
func timeoutArg(value starlark.Value) (time.Duration, error) {
	switch value := value.(type) {
	case nil, starlark.NoneType:
		return 0, nil
	case starlark.Int:
		seconds, ok := value.Int64()
		if !ok {
			return 0, errors.New("timeout is too large")
		}
		return parseTimeout(float64(seconds))
	case starlark.Float:
		return parseTimeout(float64(value))
	default:
		return 0, fmt.Errorf(
			"timeout must be a number of seconds, not %s",
			value.Type(),
		)
	}
}
