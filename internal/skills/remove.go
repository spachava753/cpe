package skills

import (
	"context"
	"fmt"
)

// RemoveOptions is unparsed command input. Remove owns selection, scope
// resolution, review, filesystem safety, removal, and bookkeeping cleanup.
type RemoveOptions struct {
	Scope  ScopeOptions
	Skills []string // Raw installed directory names; records need not exist.
	All    bool
	JSON   bool
}

// Remove is the high-level removal entry point for a command adapter.
// It will remove named installations, including unmanaged skills, delete their
// records when present, and write results through streams.
func Remove(ctx context.Context, opts RemoveOptions, streams IO) error {
	// TODO: implement removal after the exported API has been reviewed.
	return fmt.Errorf("remove skills: %w", ErrNotImplemented)
}
