package plugins

import (
	"context"
	"errors"

	"github.com/spachava753/cpe/internal/commandio"
)

// InstallOptions selects source packages, destination scope, and result format.
type InstallOptions struct {
	// Path selects a source-relative package directory and is exclusive with Plugins.
	Path *string
	// Scope requires Project or Global; Directory selects an exact custom root.
	Scope  commandio.ScopeOptions
	Source string
	// Plugins selects declared names; empty Plugins and nil Path select all.
	Plugins []string
	// Override replaces existing packages, including obsolete files.
	Override bool
	// FullDepth searches beyond the default five directory components.
	FullDepth bool
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// Install installs selected packages into the resolved scope and writes results.
// Results use terminal tables or JSON according to opts.JSON and streams.Out;
// diagnostics go to streams.Err. Output errors may follow a successful installation.
func Install(ctx context.Context, manager *Manager, opts InstallOptions, streams commandio.IO) error {
	directories, err := opts.Scope.Directories(
		"plugins",
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
			Plugins:   opts.Plugins,
			Path:      opts.Path,
			Override:  opts.Override,
			Discovery: DiscoveryOptions{
				FullDepth: opts.FullDepth,
			},
		},
	)
	if err != nil {
		if len(result) > 0 {
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
