package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/spachava753/starlarkx/starlark"
)

// maxParallel bounds concurrent host calls from one parallel() call.
const maxParallel = 16

// parallelBuiltins are the host functions parallel may run besides MCP tools.
// They share no Starlark state, so running them concurrently is safe.
var parallelBuiltins = map[string]bool{
	"bash":       true,
	"read_file":  true,
	"write_file": true,
	"edit_file":  true,
}

func fromJSONValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	err = json.Unmarshal(
		data,
		&decoded,
	)
	return decoded, err
}

// copyValue deep-copies containers so concurrent calls never share mutable
// Starlark values with each other or with the script.
func copyValue(value starlark.Value) (starlark.Value, error) {
	switch value := value.(type) {
	case starlark.NoneType, starlark.Bool, starlark.Int, starlark.Float, starlark.String, starlark.Bytes:
		return value, nil
	case *starlark.List:
		items := make(
			[]starlark.Value,
			value.Len(),
		)
		for i := range value.Len() {
			item, err := copyValue(value.Index(i))
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return starlark.NewList(items), nil
	case starlark.Tuple:
		items := make(
			starlark.Tuple,
			len(value),
		)
		for i, item := range value {
			copied, err := copyValue(item)
			if err != nil {
				return nil, err
			}
			items[i] = copied
		}
		return items, nil
	case *starlark.Dict:
		dict := starlark.NewDict(value.Len())
		for _, item := range value.Items() {
			key, err := copyValue(item[0])
			if err != nil {
				return nil, err
			}
			element, err := copyValue(item[1])
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(
				key,
				element,
			); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		return nil, fmt.Errorf(
			"parallel inputs may contain only None, bools, numbers, strings, bytes, lists, tuples, and dicts, not %s",
			value.Type(),
		)
	}
}

type parallelCall struct {
	function *starlark.Builtin
	kwargs   []starlark.Tuple
}

func (r *runtime) parallel(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"calls",
		&list,
	); err != nil {
		return nil, err
	}
	current, err := currentInvocation(thread)
	if err != nil {
		return nil, err
	}
	calls := make(
		[]parallelCall,
		list.Len(),
	)
	for i := range list.Len() {
		pair, ok := list.Index(i).(starlark.Tuple)
		if !ok || len(pair) != 2 {
			return nil, fmt.Errorf(
				"parallel: call %d must be a (function, input) tuple",
				i,
			)
		}
		function, ok := pair[0].(*starlark.Builtin)
		if !ok || !parallelBuiltins[function.Name()] && !r.isTool(function.Name()) {
			return nil, fmt.Errorf(
				"parallel: call %d must use an MCP tool, bash, read_file, write_file, or edit_file, not %s",
				i,
				pair[0].String(),
			)
		}
		input, ok := pair[1].(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf(
				"parallel: call %d input must be a dictionary of keyword arguments",
				i,
			)
		}
		calls[i].function = function
		for _, item := range input.Items() {
			key, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf(
					"parallel: call %d input keys must be strings",
					i,
				)
			}
			value, err := copyValue(item[1])
			if err != nil {
				return nil, fmt.Errorf(
					"parallel: call %d: %w",
					i,
					err,
				)
			}
			calls[i].kwargs = append(
				calls[i].kwargs,
				starlark.Tuple{
					key,
					value,
				},
			)
		}
	}
	ctx, cancel := context.WithCancel(current.ctx)
	defer cancel()
	child := &invocation{
		ctx:       ctx,
		callState: current.callState,
	}
	results := make(
		[]starlark.Value,
		len(calls),
	)
	errs := make(
		[]error,
		len(calls),
	)
	slots := make(
		chan struct{},
		maxParallel,
	)
	var wait sync.WaitGroup
	for i, call := range calls {
		slots <- struct{}{}
		if ctx.Err() != nil {
			<-slots
			break
		}
		wait.Go(func() {
			defer func() { <-slots }()
			callThread := newThread(child)
			result, err := starlark.Call(
				callThread,
				call.function,
				nil,
				call.kwargs,
			)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			result.Freeze()
			results[i] = result
		})
	}
	wait.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(
			err,
			context.Canceled,
		) {
			return nil, fmt.Errorf(
				"parallel: call %d: %w",
				i,
				err,
			)
		}
	}
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf(
				"parallel: call %d: %w",
				i,
				err,
			)
		}
	}
	if err := current.ctx.Err(); err != nil {
		return nil, err
	}
	return starlark.NewList(results), nil
}

func (r *runtime) isTool(name string) bool {
	_, ok := r.config.Catalog.Tool(name)
	return ok
}
