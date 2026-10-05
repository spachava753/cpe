package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// RemoveOptions is unparsed command input. Remove owns invocation parsing,
// destination resolution, and output; Manager owns removal.
type RemoveOptions struct {
	Scope  ScopeOptions
	Skills []string // Raw installed directory names.
	All    bool
	JSON   bool
}

// Remove is the high-level removal entry point for a command adapter.
// It removes named installed skill directories and writes results through streams.
func Remove(ctx context.Context, manager *Manager, opts RemoveOptions, streams IO) error {
	directories, err := resolveDirectories(
		opts.Scope,
		false,
	)
	if err != nil {
		return err
	}
	names, err := selectedNames(opts.Skills)
	if err != nil {
		return err
	}
	if opts.All && len(names) > 0 {
		return errors.New("all cannot be combined with selected skill names")
	}
	if !opts.All && len(names) == 0 {
		return errors.New("select at least one skill to remove")
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
				info, err := manager.pathInfo(filepath.Join(
					directory,
					name,
				))
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
						"%s is not a skill directory",
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
					"skill %q not found: %w",
					name,
					fs.ErrNotExist,
				)
			}
			if len(matches) > 1 {
				return fmt.Errorf(
					"skill %q exists in both directories; --scope is required",
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
				Skills:    selected[directory],
			},
		); err != nil {
			return err
		}
	}
	var result []RemovedSkill
	for _, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		removed, err := manager.Remove(
			ctx,
			RemoveRequest{
				Directory: directory,
				Skills:    selected[directory],
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
						"some skills changes committed: %w",
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
