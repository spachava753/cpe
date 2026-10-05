package skills

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ListOptions is unparsed listing input. List owns destination resolution and
// text/JSON formatting; Manager owns source discovery and installed inventory.
type ListOptions struct {
	Scope ScopeOptions
	// Source selects available skills in a Git source instead of installed
	// inventory. Nil means omitted; non-nil preserves an explicit argument.
	// Scope applies only to installed inventory.
	Source *string
	JSON   bool
}

// List is the high-level listing entry point for a command adapter. It can
// list available source skills or installed skills, including manually copied ones,
// and write results through streams. JSON output returns an error for metadata
// values JSON cannot represent, such as non-finite YAML numbers.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams IO) error {
	if opts.Source != nil {
		result, err := manager.ListSource(
			ctx,
			*opts.Source,
			DiscoveryOptions{},
		)
		if err != nil {
			return err
		}
		var violationErr error
		for _, skill := range result {
			if violationErr = writeViolations(
				streams,
				skill,
			); violationErr != nil {
				break
			}
		}
		output := outputStream(streams)
		if opts.JSON {
			if result == nil {
				result = []Skill{}
			}
			return errors.Join(
				violationErr,
				json.NewEncoder(output).Encode(result),
			)
		}
		var rows [][]string
		for _, skill := range result {
			rows = append(
				rows,
				[]string{
					strings.TrimSpace(displayText(skill.Name)),
					strings.TrimSpace(displayText(skill.Description)),
					displayText(skill.Path),
				},
			)
		}
		return errors.Join(
			violationErr,
			writeTable(
				output,
				[]string{
					"Skill",
					"Description",
					locationColumn,
				},
				rows,
			),
		)
	}
	directories, err := resolveDirectories(
		opts.Scope,
		false,
	)
	if err != nil {
		return err
	}
	var result []InstalledSkill
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
	return writeInstalled(
		streams,
		opts.JSON,
		result,
	)
}
