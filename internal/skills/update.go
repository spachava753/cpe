package skills

import (
	"context"
	"fmt"
)

// UpdateOptions is unparsed command input. Update owns invocation parsing,
// destination resolution, prompting, and output; Manager owns source acquisition
// and updates to managed installations.
type UpdateOptions struct {
	Scope  ScopeOptions
	Skills []string // Empty selects all managed skills in the chosen scope.
	JSON   bool
}

// Update is the high-level update entry point for a command adapter.
// It will update managed installations and write results through streams.
func Update(ctx context.Context, manager *Manager, opts UpdateOptions, streams IO) error {
	// TODO: implement updates after the exported API has been reviewed.
	return fmt.Errorf("update skills: %w", ErrNotImplemented)
}
