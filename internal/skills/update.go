package skills

import "context"

// UpdateOptions is unparsed command input. Update owns invocation parsing,
// destination resolution, prompting, and output; Manager owns source acquisition
// and updates to managed installations.
type UpdateOptions struct {
	Scope  ScopeOptions
	Skills []string // Empty selects all managed skills in the chosen scope.
	JSON   bool
}

// Update is the high-level update entry point for a command adapter.
// It updates managed installations and writes results through streams.
func Update(ctx context.Context, manager *Manager, opts UpdateOptions, streams IO) error {
	if err := updateCommand(
		ctx,
		manager,
		opts,
		streams,
	); err != nil {
		return &commandError{
			action: "update",
			cause:  err,
		}
	}
	return nil
}
