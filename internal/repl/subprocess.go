package repl

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"
)

const subprocessOutcome = "builtin.subprocess.run"

// subprocessModule records the builtin's outcome after Dyson's timeout check,
// which happens after the host runner returns (and after its result is synced).
// Host success alone therefore cannot determine whether the CALL succeeded.
func (j *journal) subprocessModule(source dyson.SphereSource) dyson.ModuleSet {
	original := source.Modules()["subprocess.star"]["subprocess"].(*starlarkstruct.Module) //nolint:forcetypeassert // Pinned Dyson constructs this namespace as a Module.
	members := maps.Clone(original.Members)
	run := members["run"]
	members["run"] = starlark.NewBuiltin("subprocess.run", func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		arguments := struct {
			Args     string
			Keywords []string
		}{Args: args.String()}
		for _, kw := range kwargs {
			arguments.Keywords = append(arguments.Keywords, kw.String())
		}
		if j.replay {
			// Argument errors have no runner record. They are terminal builtin outcomes
			// too; replay them without accidentally reaching a host after removing timeout.
			if j.position < len(j.records) {
				var next hostCall
				if json.Unmarshal(j.records[j.position].Data, &next) == nil && next.Name == subprocessOutcome {
					// Independently check the original arguments. A valid call
					// must reach command.run and fail journal matching here;
					// absent host records cannot establish a validation failure.
					_, callErr := starlark.Call(thread, run, args, kwargs)
					if j.fatal != nil {
						return nil, j.fatal
					}
					outcomeErr := effect(j, subprocessOutcome, arguments, func() error { return callErr })
					if callErr == nil || outcomeErr == nil || callErr.Error() != outcomeErr.Error() {
						j.fatal = errors.New("subprocess replay argument outcome differs")
						return nil, j.fatal
					}
					return nil, outcomeErr
				}
			}
			// The pinned run signature places timeout at positional index 8. Keep all
			// original arguments in the outcome record; only its replay invocation omits
			// the timer. Dyson still reconstructs the native CompletedProcess value.
			args = slices.Clone(args)
			if len(args) > 8 {
				args[8] = starlark.None
			}
			kwargs = slices.Clone(kwargs)
			for i, kw := range kwargs {
				if kw[0] == starlark.String("timeout") {
					kwargs[i] = starlark.Tuple{kw[0], starlark.None}
				}
			}
		}
		value, callErr := starlark.Call(thread, run, args, kwargs)
		outcomeErr := effect(j, subprocessOutcome, arguments, func() error { return callErr })
		if outcomeErr != nil {
			return nil, outcomeErr
		}
		if callErr != nil {
			j.fatal = errors.New("subprocess replay differs from successful builtin outcome")
			return nil, j.fatal
		}
		return value, nil
	})
	module := &starlarkstruct.Module{Name: original.Name, Members: members}
	module.Freeze()
	return dyson.ModuleSet{"subprocess.star": {"subprocess": module}}
}
