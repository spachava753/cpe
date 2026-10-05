package skills

import (
	"context"
	"errors"
)

// InstallOptions is unparsed command input. Install owns invocation parsing,
// destination resolution, and output; Manager owns skill operations.
// Source is the required raw source argument; Skills are repeated --skill values.
// Path is the optional --path value, an exact source-relative skill directory.
type InstallOptions struct {
	Scope     ScopeOptions
	Source    string
	Skills    []string
	Path      *string
	FullDepth bool
	JSON      bool
}

// Install is the high-level installation entry point for a command adapter.
// It resolves the destination and writes results through streams.
func Install(ctx context.Context, manager *Manager, opts InstallOptions, streams IO) error {
	directories, err := resolveDirectories(
		opts.Scope,
		true,
	)
	if err != nil {
		return err
	}
	result, err := manager.Install(
		ctx,
		InstallRequest{
			Source:    opts.Source,
			Directory: directories[0],
			Skills:    opts.Skills,
			Path:      opts.Path,
			Discovery: DiscoveryOptions{
				FullDepth: opts.FullDepth,
			},
		},
	)
	if err != nil {
		if _, committed := errors.AsType[*CommittedCleanupError](err); committed {
			return errors.Join(
				err,
				writeInstalled(
					streams,
					opts.JSON,
					result,
				),
			)
		}
		return err
	}
	return writeInstalled(
		streams,
		opts.JSON,
		result,
	)
}
