package skills

import "context"

// ListOptions is unparsed listing input. List owns destination resolution and
// text/JSON formatting; Manager owns source discovery and installed inventory.
type ListOptions struct {
	Scope ScopeOptions
	// Source selects available skills in a Git source instead of installed
	// inventory. Nil means omitted; non-nil preserves an explicit argument.
	// Scope applies only to installed inventory.
	Source *string
	JSON   bool
}

// List is the high-level listing entry point for a command adapter. It can
// list available source skills or installed skills, including unmanaged ones,
// and write results through streams.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams IO) error {
	if err := listCommand(
		ctx,
		manager,
		opts,
		streams,
	); err != nil {
		return &commandError{
			action: "list",
			cause:  err,
		}
	}
	return nil
}
