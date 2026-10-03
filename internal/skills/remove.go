package skills

import (
	"context"
	"fmt"
)

// RemoveOptions is unparsed command input. Remove owns invocation parsing,
// destination resolution, prompting, and output; Manager owns removal and records.
type RemoveOptions struct {
	Scope  ScopeOptions
	Skills []string // Raw installed directory names; records need not exist.
	All    bool
	JSON   bool
}

// Remove is the high-level removal entry point for a command adapter.
// It will remove named installations, including unmanaged skills, delete their
// records when present, and write results through streams.
func Remove(ctx context.Context, manager *Manager, opts RemoveOptions, streams IO) error {
	// TODO: implement removal after the exported API has been reviewed.
	return fmt.Errorf("remove skills: %w", ErrNotImplemented)
}
