package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/commandio"
)

// RemoveOptions selects installed package directories and their output format.
type RemoveOptions struct {
	// Scope selects roots to search; its zero value searches project and global roots.
	Scope commandio.ScopeOptions
	// Plugins contains installed directory names. A name in both roots is ambiguous.
	Plugins []string
	// All selects every listed package and is mutually exclusive with Plugins.
	All bool
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// Remove deletes selected packages and writes summaries to streams.Out.
// Results are terminal tables or JSON arrays containing name, description, and
// former package-directory path. Errors can follow committed removals or result writes.
func Remove(ctx context.Context, manager *Manager, opts RemoveOptions, streams commandio.IO) error {
	directories, err := opts.Scope.Directories(
		"plugins",
		false,
	)
	if err != nil {
		return err
	}
	names, err := selectedNames(opts.Plugins)
	if err != nil {
		return err
	}
	if opts.All && len(names) > 0 {
		return errors.New("all cannot be combined with selected plugin names")
	}
	if !opts.All && len(names) == 0 {
		return errors.New("select at least one plugin to remove")
	}
	selected := make(map[string][]string)
	if opts.All {
		for _, directory := range directories {
			installed, err := manager.ListInstalled(
				ctx,
				directory,
			)
			if err != nil {
				return err
			}
			for _, entry := range installed {
				selected[directory] = append(
					selected[directory],
					filepath.Base(entry.Directory),
				)
			}
		}
	} else {
		for _, name := range names {
			var matches []string
			for _, directory := range directories {
				if err := manager.destination(
					ctx,
					directory,
				); err != nil {
					return err
				}
				info, err := bundlefs.Lstat(
					manager.destinations,
					filepath.Join(
						directory,
						name,
					),
				)
				if errors.Is(
					err,
					fs.ErrNotExist,
				) {
					continue
				}
				if err != nil {
					return err
				}
				if !info.IsDir() {
					return fmt.Errorf(
						"%s is not a plugin directory",
						filepath.Join(
							directory,
							name,
						),
					)
				}
				matches = append(
					matches,
					directory,
				)
			}
			if len(matches) == 0 {
				return fmt.Errorf(
					"plugin %q not found: %w",
					name,
					fs.ErrNotExist,
				)
			}
			if len(matches) > 1 {
				return fmt.Errorf(
					"plugin %q exists in both directories; --scope is required",
					name,
				)
			}
			selected[matches[0]] = append(
				selected[matches[0]],
				name,
			)
		}
	}
	// Check every selection's eligibility before removing the first scope.
	for _, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		if _, err := manager.prepareRemoval(
			ctx,
			RemoveRequest{
				Directory: directory,
				Plugins:   selected[directory],
			},
		); err != nil {
			return err
		}
	}
	var result []RemovedPlugin
	for _, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		removed, err := manager.Remove(
			ctx,
			RemoveRequest{
				Directory: directory,
				Plugins:   selected[directory],
			},
		)
		result = append(
			result,
			removed...,
		)
		if err != nil {
			if len(result) > 0 {
				return errors.Join(
					fmt.Errorf(
						"some plugins changes committed: %w",
						err,
					),
					writeRemoved(
						streams,
						opts.JSON,
						result,
					),
				)
			}
			return err
		}
	}
	return writeRemoved(
		streams,
		opts.JSON,
		result,
	)
}
