package repl

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/syntax"
)

// ErrSteered is a context cancellation cause. A settled evaluation interrupted
// for this reason retains its partial interpreter state and a replay cutoff.
var ErrSteered = errors.New("tool execution canceled due to a steered prompt")

// interruption distinguishes stopping BEFORE an opcode from an error returned
// BY that opcode's builtin. The latter must replay the host call and its result.
type interruption struct {
	Steps        uint64 `json:"steps"`
	BeforeOpcode bool   `json:"beforeOpcode"`
}

// evaluator owns execution, while Dyson owns the standard-library values and
// resources. Owning the Thread lets cancellation and replay use the same precise
// opcode boundary, without an asynchronous Thread.Cancel race.
type evaluator struct {
	thread  *starlark.Thread
	globals starlark.StringDict
	sources []dyson.SphereSource
}

func newEvaluator(print func(*starlark.Thread, string), sources ...dyson.SphereSource) *evaluator {
	modules := map[string]starlark.StringDict{}
	globals := starlark.StringDict{}
	for _, source := range sources {
		maps.Copy(modules, source.Modules())
		maps.Copy(globals, source.Globals())
	}
	return &evaluator{globals: globals, sources: sources, thread: &starlark.Thread{
		Name: "cpe.repl", Print: print,
		Load: func(_ *starlark.Thread, name string) (starlark.StringDict, error) {
			if values, ok := modules[name]; ok {
				return values, nil
			}
			return nil, fmt.Errorf("load: module %q is not registered", name)
		},
	}}
}

func (e *evaluator) Close() error {
	var errs []error
	for _, source := range e.sources {
		errs = append(errs, source.Close())
	}
	e.sources = nil
	return errors.Join(errs...)
}

func (e *evaluator) Eval(ctx context.Context, code string, cutoff *interruption) (steps uint64, beforeOpcode bool, err error) {
	t := e.thread
	t.Uncancel()
	t.Steps = 0
	t.SetMaxExecutionSteps(1)
	t.OnMaxSteps = func(t *starlark.Thread) {
		switch {
		case ctx.Err() != nil:
			beforeOpcode = true
			t.Cancel(context.Cause(ctx).Error())
		case cutoff != nil && cutoff.BeforeOpcode && t.ExecutionSteps() == cutoff.Steps:
			beforeOpcode = true
			t.Cancel(ErrSteered.Error())
		case cutoff != nil && t.ExecutionSteps() > cutoff.Steps:
			t.Cancel("replay exceeded recorded interruption")
		}
	}
	opts := &syntax.FileOptions{Set: true, While: true, TopLevelControl: true, GlobalReassign: true, LoadBindsGlobally: true}
	file, err := opts.Parse("<cpe_repl>", code, 0)
	if err == nil {
		err = starlark.ExecREPLChunk(file, t, e.globals)
	}
	return t.ExecutionSteps(), beforeOpcode, err
}
