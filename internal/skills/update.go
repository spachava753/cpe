package skills

import (
	"context"
	"fmt"
)

// UpdateOptions is unparsed command input. Update owns selection, scope
// resolution, source retrieval, and updates to installations tracked in skills.json.
type UpdateOptions struct {
	Scope  ScopeOptions
	Skills []string // Empty selects all managed skills in the chosen scope.
	JSON   bool
}

// Update is the high-level update entry point for a command adapter.
// It will update managed installations and write results through streams.
func Update(ctx context.Context, opts UpdateOptions, streams IO) error {
	// TODO: implement updates after the exported API has been reviewed.
	return fmt.Errorf("update skills: %w", ErrNotImplemented)
}
