package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/commandio"
)

// RemoveOptions selects installed skill directories and their output format.
type RemoveOptions struct {
	// Scope selects roots to search; its zero value searches project and global roots.
	Scope commandio.ScopeOptions
	// Skills contains installed directory names. A name in both roots is ambiguous.
	Skills []string
	// All selects every listed skill and is mutually exclusive with Skills.
	All bool
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// Remove deletes selected skill bundles and writes summaries to streams.Out.
// Results are terminal tables or JSON arrays containing name, description, and
// former SKILL.md path. Errors can follow committed removals or result writes.
func Remove(ctx context.Context, manager *Manager, opts RemoveOptions, streams commandio.IO) error {
	if manager == nil {
		return errors.New("skills manager is unavailable")
	}
	scopes, err := resolveScopes(
		opts.Scope,
		false,
	)
	if err != nil {
		return err
	}
	var directories []string
	locks := map[string]LockFile{}
	for _, scope := range scopes {
		directories = append(
			directories,
			scope.directory,
		)
		locks[scope.directory] = scope.lock
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
					"skill %q exists in both directories; provide an explicit --scope",
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
		if _, err := manager.readLock(
			ctx,
			locks[directory],
		); err != nil {
			return err
		}
	}
	for first, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		for _, other := range directories[first+1:] {
			if len(selected[other]) == 0 {
				continue
			}
			same, err := bundlefs.SameDirectory(
				manager.destinations,
				directory,
				other,
			)
			if err != nil {
				return err
			}
			if directory == other || same {
				return errors.New("project and global inventories share a directory; provide an explicit --scope to remove skills")
			}
		}
	}
	var result []RemovedSkill
	for _, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		lock := locks[directory]
		removed, err := manager.Remove(
			ctx,
			RemoveRequest{
				Directory: directory,
				Skills:    selected[directory],
				Lock:      &lock,
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
