package toolruntime

import (
	"context"
	"errors"
	"strings"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"
)

const compactName = "compact"

type compactionKey struct{}

// WithCompaction supplies the receiver for summaries accepted by compact during a turn.
// The receiver runs synchronously; the caller decides when to checkpoint the summary.
func WithCompaction(ctx context.Context, accept func(string)) context.Context {
	return context.WithValue(
		ctx,
		compactionKey{},
		accept,
	)
}

func compact(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var summary string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"summary",
		&summary,
	); err != nil {
		return nil, err
	}
	if strings.TrimSpace(summary) == "" {
		return nil, errors.New("summary must be a nonempty string")
	}
	ctx := dyson.EvaluationContext(thread)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	accept, ok := ctx.Value(compactionKey{}).(func(string))
	if !ok || accept == nil {
		return nil, errors.New("compact requires an active prompt")
	}
	accept(summary)
	return starlark.None, nil
}
