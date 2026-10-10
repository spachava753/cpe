package skills

import (
	"context"

	"github.com/spachava753/cpe/internal/commandio"
)

// UpdateOptions selects installed skills and their result format.
type UpdateOptions struct {
	// Scope restricts inventory; its zero value searches project and global roots.
	Scope commandio.ScopeOptions
	// Skills contains installed directory names. Empty selects every installed skill.
	Skills []string
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// Update replaces tracked skills from their recorded Git sources and writes results.
// Missing, ambiguous, and untracked selections are warned about and skipped.
func Update(ctx context.Context, manager *Manager, opts UpdateOptions, streams commandio.IO) error {
	panic("not implemented")
}
