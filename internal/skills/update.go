package skills

import (
	"context"
	"errors"
	"fmt"
)

// UpdateOptions is unparsed command input. Update owns invocation parsing,
// destination resolution, and output; Manager owns source acquisition
// and updates to managed installations.
type UpdateOptions struct {
	Scope  ScopeOptions
	Skills []string // Empty selects all managed skills in the chosen scope.
	JSON   bool
}

// Update is the high-level update entry point for a command adapter.
// It updates managed installations and writes results through streams.
func Update(ctx context.Context, manager *Manager, opts UpdateOptions, streams IO) error {
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
	selected := make(map[string][]string)
	found := make(map[string]bool)
	for _, directory := range directories {
		if err := manager.destination(
			ctx,
			directory,
		); err != nil {
			return err
		}
		records, err := manager.records(directory)
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, ok := records.Skills[name]; ok {
				selected[directory] = append(
					selected[directory],
					name,
				)
				found[name] = true
			}
		}
	}
	for _, name := range names {
		if !found[name] {
			return fmt.Errorf(
				"skill %q is not managed",
				name,
			)
		}
	}
	var result []InstalledSkill
	for _, directory := range directories {
		if len(names) > 0 && len(selected[directory]) == 0 {
			continue
		}
		updated, err := manager.Update(
			ctx,
			UpdateRequest{
				Directory: directory,
				Skills:    selected[directory],
			},
		)
		result = append(
			result,
			updated...,
		)
		if err != nil {
			if len(result) > 0 {
				return errors.Join(
					fmt.Errorf(
						"some skills changes committed: %w",
						err,
					),
					writeInstalled(
						streams,
						opts.JSON,
						result,
					),
				)
			}
			return err
		}
	}
	return writeInstalled(
		streams,
		opts.JSON,
		result,
	)
}
