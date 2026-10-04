package skills

import "context"

// InstallOptions is unparsed command input. Install owns invocation parsing,
// destination resolution, prompting, and output; Manager owns skill operations.
// Source is the required raw source argument; Skills are repeated --skill values.
type InstallOptions struct {
	Scope     ScopeOptions
	Source    string
	Skills    []string
	FullDepth bool
	JSON      bool
}

// Install is the high-level installation entry point for a command adapter.
// It resolves the destination and writes results through streams.
func Install(ctx context.Context, manager *Manager, opts InstallOptions, streams IO) error {
	if err := installCommand(
		ctx,
		manager,
		opts,
		streams,
	); err != nil {
		return &commandError{
			action: "install",
			cause:  err,
		}
	}
	return nil
}
