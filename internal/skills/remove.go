package skills

import "context"

// RemoveOptions is unparsed command input. Remove owns invocation parsing,
// destination resolution, prompting, and output; Manager owns removal and records.
type RemoveOptions struct {
	Scope  ScopeOptions
	Skills []string // Raw installed directory names; records need not exist.
	All    bool
	JSON   bool
}

// Remove is the high-level removal entry point for a command adapter.
// It removes named installations, including unmanaged skills, deletes their
// records when present, and writes results through streams.
func Remove(ctx context.Context, manager *Manager, opts RemoveOptions, streams IO) error {
	if err := removeCommand(ctx, manager, opts, streams); err != nil {
		return &commandError{action: "remove", cause: err}
	}
	return nil
}
