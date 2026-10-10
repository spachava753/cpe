package skills

import (
	"context"
	"errors"

	"github.com/spachava753/cpe/internal/commandio"
)

// InstallOptions selects source skills, destination scope, and result format.
type InstallOptions struct {
	// Scope requires Project or Global; Directory selects an exact custom root.
	Scope  commandio.ScopeOptions
	Source string
	// Skills selects declared names; empty Skills and nil Path select all.
	Skills []string
	// Path selects a source-relative bundle directory and is exclusive with Skills.
	Path *string
	// Override replaces existing bundles, including obsolete files.
	Override bool
	// FullDepth searches beyond the default five directory components.
	FullDepth bool
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// Install installs selected skills into the resolved scope and writes results.
// Results use terminal tables or JSON according to opts.JSON and streams.Out;
// skill warnings go to streams.Err. Output errors may follow a successful installation.
func Install(ctx context.Context, manager *Manager, opts InstallOptions, streams commandio.IO) error {
	scopes, err := resolveScopes(
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
			Directory: scopes[0].directory,
			Lock:      &scopes[0].lock,
			Skills:    opts.Skills,
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
