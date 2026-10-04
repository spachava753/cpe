package skills

import (
	"context"
	"errors"
)

// InstallOptions is unparsed command input. Install owns invocation parsing,
// destination resolution, and output; Manager owns skill operations.
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
