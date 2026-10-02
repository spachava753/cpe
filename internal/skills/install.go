package skills

import (
	"context"
	"fmt"
)

// InstallOptions is unparsed command input. Install owns source interpretation,
// domain parsing, discovery, selection, scope resolution, review, and mutation.
// Source is the required raw source argument; Skills are repeated --skill values.
type InstallOptions struct {
	Scope     ScopeOptions
	Source    string
	Skills    []string
	FullDepth bool
	JSON      bool
}

// Install is the high-level installation entry point for a command adapter.
// It will handle the complete operation and write results through streams.
func Install(ctx context.Context, opts InstallOptions, streams IO) error {
	// TODO: implement installation after the exported API has been reviewed.
	return fmt.Errorf("install skills: %w", ErrNotImplemented)
}
