package plugins

import "context"

// ListOptions is unparsed listing input. List owns destination resolution and
// text/JSON formatting; Manager owns source discovery and installed inventory.
type ListOptions struct {
	Scope ScopeOptions
	// Source selects available plugins in a Git source instead of installed
	// inventory. Nil means omitted; non-nil preserves an explicit argument.
	// Scope applies only to installed inventory.
	Source *string
	JSON   bool
}

// List lists available source plugins or installed plugins, including manually
// copied ones. JSON exposes summaries; diagnostics are written separately.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams IO) error {
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
		directories, err := resolveDirectories(
			opts.Scope,
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
