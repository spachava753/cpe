package plugins

import (
	"context"

	"github.com/spachava753/cpe/internal/commandio"
)

// ListOptions selects installed or source packages and their output format.
type ListOptions struct {
	// Scope selects installed roots; its zero value lists project and global roots.
	Scope commandio.ScopeOptions
	// Source selects source discovery when non-nil, ignoring Scope.
	Source *string
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// List writes source or installed plugin summaries to streams.Out and diagnostics
// to streams.Err. Results are terminal tables or JSON arrays containing name,
// description, and package-directory path.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams commandio.IO) error {
	var result []InstalledPlugin
	if opts.Source != nil {
		available, err := manager.ListSource(
			ctx,
			*opts.Source,
			DiscoveryOptions{},
		)
		if err != nil {
			return err
		}
		for _, plugin := range available {
			result = append(
				result,
				InstalledPlugin{
					Plugin:    plugin,
					Directory: plugin.Path,
				},
			)
		}
	} else {
		directories, err := opts.Scope.Directories(
			"plugins",
			false,
		)
		if err != nil {
			return err
		}
		for _, directory := range directories {
			installed, err := manager.ListInstalled(
				ctx,
				directory,
			)
			if err != nil {
				return err
			}
			result = append(
				result,
				installed...,
			)
		}
	}
	return writeInstalled(
		streams,
		opts.JSON,
		result,
	)
}
